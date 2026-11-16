package core

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	stdlog "log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/dangduoc08/ginject/internal/color"
	"github.com/dangduoc08/ginject/routing"
)

const (
	defaultMaxRequestBodyBytes = 10 << 20
	defaultShutdownTimeout     = 30 * time.Second
)

var (
	errNotCreated        = errors.New("core: Create must be called before serving")
	errAlreadyServing    = errors.New("core: app is already serving")
	errStopped           = errors.New("core: app is stopped")
	errTLSConfigOnListen = errors.New("core: TLSConfig is set: use ListenTLS, or Serve with your listener")
	errH2CWithTLS        = errors.New("core: H2C applies to connections without TLS")
	errNoCertificate     = errors.New("core: ListenTLS needs certFile and keyFile, or a TLSConfig with certificates")
)

type ServerOptions struct {
	ShutdownTimeout     time.Duration
	MaxRequestBodyBytes int64
	TLSConfig           *tls.Config
	H2C                 bool
}

type serverDefaults struct {
	readHeaderTimeout, readTimeout, writeTimeout, idleTimeout time.Duration
	maxHeaderBytes                                            int
}

type serveState uint8

const (
	serveCreated serveState = iota
	serveServing
	serveStopping
	serveStopped
)

func (app *App) Listen(port int, opts *ServerOptions) error {
	if err := app.servingError(); err != nil {
		return err
	}
	o := loadServerOptions(opts)
	if o.TLSConfig != nil {
		return errTLSConfigOnListen
	}

	return app.listenAndServe(port, o)
}

func (app *App) ListenTLS(port int, certFile, keyFile string, opts *ServerOptions) error {
	if err := app.servingError(); err != nil {
		return err
	}
	o := loadServerOptions(opts)
	if o.H2C {
		return errH2CWithTLS
	}

	if certFile != "" || keyFile != "" {
		if certFile == "" || keyFile == "" {
			return errNoCertificate
		}
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return fmt.Errorf("core: load TLS certificate: %w", err)
		}
		if o.TLSConfig == nil {
			o.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		}
		o.TLSConfig.Certificates = []tls.Certificate{cert}
	} else if !hasCertificateSource(o.TLSConfig) {
		return errNoCertificate
	}

	return app.listenAndServe(port, o)
}

func (app *App) Serve(l net.Listener, opts *ServerOptions) error {
	o := loadServerOptions(opts)
	if o.TLSConfig != nil {
		if o.H2C {
			return errH2CWithTLS
		}
		if !hasCertificateSource(o.TLSConfig) {
			return errNoCertificate
		}
	}

	return app.serve(l, o)
}

func (app *App) Stop() {
	app.serveMu.Lock()
	switch app.serveState {
	case serveCreated:
		app.serveState = serveStopped
		app.serveMu.Unlock()
		app.runShutdownHooks()
		return
	case serveServing:
		app.serveState = serveStopping
		close(app.stopRequest)
	}
	isRunningHooks := app.isRunningHooks
	app.serveMu.Unlock()

	if !isRunningHooks {
		<-app.shutdownDone
	}
}

func newServerDefaults() serverDefaults {
	return serverDefaults{
		readHeaderTimeout: 5 * time.Second,
		readTimeout:       30 * time.Second,
		writeTimeout:      30 * time.Second,
		idleTimeout:       75 * time.Second,
		maxHeaderBytes:    64 << 10,
	}
}

func notifyShutdownSignals() (<-chan os.Signal, func()) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	return signals, func() { signal.Stop(signals) }
}

func loadServerOptions(opts *ServerOptions) ServerOptions {
	var o ServerOptions
	if opts != nil {
		o = *opts
	}
	if o.ShutdownTimeout <= 0 {
		o.ShutdownTimeout = defaultShutdownTimeout
	}
	if o.MaxRequestBodyBytes == 0 {
		o.MaxRequestBodyBytes = defaultMaxRequestBodyBytes
	}
	if o.TLSConfig != nil {
		o.TLSConfig = o.TLSConfig.Clone()
	}

	return o
}

func hasCertificateSource(cfg *tls.Config) bool {
	return cfg != nil && (len(cfg.Certificates) > 0 || cfg.GetCertificate != nil || cfg.GetConfigForClient != nil)
}

func (app *App) servingError() error {
	app.serveMu.Lock()
	defer app.serveMu.Unlock()

	return app.servingErrorLocked()
}

func (app *App) servingErrorLocked() error {
	switch {
	case app.module == nil:
		return errNotCreated
	case app.serveState == serveServing || app.serveState == serveStopping:
		return errAlreadyServing
	case app.serveState == serveStopped:
		return errStopped
	}

	return nil
}

func (app *App) listenAndServe(port int, opts ServerOptions) error {
	address := net.JoinHostPort("", strconv.Itoa(port))
	l, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("core: listen on %s: %w", address, err)
	}
	defer func() { _ = l.Close() }()

	return app.serve(l, opts)
}

func (app *App) serve(l net.Listener, opts ServerOptions) error {
	stopRequest, err := app.beginServing()
	if err != nil {
		return err
	}

	app.maxRequestBodyBytes.Store(opts.MaxRequestBodyBytes)
	isTLS := opts.TLSConfig != nil
	server := app.newServer(opts, isTLS)
	app.logStartup(l.Addr(), isTLS, protocolNames(server.Protocols))

	signals, stopSignals := app.notifySignals()
	var serveErr error
	served := make(chan struct{})
	go func() {
		defer close(served)
		if isTLS {
			serveErr = server.ServeTLS(l, "", "")
			return
		}
		serveErr = server.Serve(l)
	}()

	select {
	case sig := <-signals:
		stopSignals()
		app.markStopping()
		app.Logger.Info("ShutdownSignal", "signal", sig.String())
		err = drain(server, opts.ShutdownTimeout)
		<-served
	case <-stopRequest:
		stopSignals()
		app.Logger.Info("Shutdown")
		err = drain(server, opts.ShutdownTimeout)
		<-served
	case <-served:
		stopSignals()
		app.markStopping()
		_ = drain(server, opts.ShutdownTimeout)
		err = fmt.Errorf("core: serve: %w", serveErr)
	}

	app.runShutdownHooks()

	return err
}

func (app *App) beginServing() (<-chan struct{}, error) {
	app.serveMu.Lock()
	defer app.serveMu.Unlock()

	if err := app.servingErrorLocked(); err != nil {
		return nil, err
	}
	app.serveState = serveServing
	app.stopRequest = make(chan struct{})

	return app.stopRequest, nil
}

func (app *App) newServer(opts ServerOptions, isTLS bool) *http.Server {
	var protocols http.Protocols
	protocols.SetHTTP1(true)
	switch {
	case isTLS:
		protocols.SetHTTP2(allowsHTTP2(opts.TLSConfig))
	case opts.H2C:
		protocols.SetUnencryptedHTTP2(true)
	}

	d := app.serverDefaults

	return &http.Server{
		Handler:           app,
		ReadHeaderTimeout: d.readHeaderTimeout,
		ReadTimeout:       d.readTimeout,
		WriteTimeout:      d.writeTimeout,
		IdleTimeout:       d.idleTimeout,
		MaxHeaderBytes:    d.maxHeaderBytes,
		TLSConfig:         opts.TLSConfig,
		Protocols:         &protocols,
		ErrorLog:          stdlog.New(serverErrorWriter{app: app}, "", 0),
	}
}

func allowsHTTP2(cfg *tls.Config) bool {
	return cfg == nil || len(cfg.NextProtos) == 0 || slices.Contains(cfg.NextProtos, "h2")
}

type serverErrorWriter struct {
	app *App
}

func (w serverErrorWriter) Write(p []byte) (int, error) {
	w.app.Logger.Warn("HTTPServer", "message", strings.TrimSuffix(string(p), "\n"))

	return len(p), nil
}

func protocolNames(p *http.Protocols) string {
	switch {
	case p.HTTP2():
		return "HTTP/1.1, HTTP/2"
	case p.UnencryptedHTTP2():
		return "HTTP/1.1, h2c"
	}

	return "HTTP/1.1"
}

func (app *App) logStartup(addr net.Addr, isTLS bool, protocols string) {
	for _, m := range app.module.collectModules() {
		if m.Name != "" {
			app.Logger.Info("InstanceLoader", "module", m.Name+" initialized")
		}
	}

	patterns := make([]string, 0, len(app.module.HTTPMainHandlers))
	for _, h := range app.module.HTTPMainHandlers {
		patterns = append(patterns, h.Pattern)
	}
	sort.Strings(patterns)

	for _, pattern := range patterns {
		m, r, v := routing.PatternToMethodRouteVersion(pattern)
		if r == "" {
			r = "/"
		}
		args := []any{"method", m, "route", r}
		if v != "" {
			args = append(args, "version", v)
		}
		app.Logger.Info("RouteExplorer", args...)
	}

	if app.isWSEnabled {
		for _, h := range app.module.WSMainHandlers {
			app.Logger.Info("WebSocketEvent", "event", h.EventName)
		}
	}

	app.Logger.Info("ServerListening", "address", addr.String(), "tls", isTLS, "protocols", protocols)

	if tcpAddr, ok := addr.(*net.TCPAddr); ok && isTerminal(os.Stdout) {
		logBoostrap(tcpAddr.Port)
	}
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()

	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func logBoostrap(port int) {
	accessURLs := color.FmtBold("%s", color.FmtBGYellow(" GG! Here Are Your Access URLs: ")) + "\n"
	divider := color.FmtDim("--------------------------------------------") + "\n"
	host := color.FmtBold("%s", color.FmtWhite("Localhost: ")) + color.FmtMagenta("%v:%v", "localhost", port) + "\n"
	lan := color.FmtBold("%s", color.FmtWhite("      LAN: ")) + color.FmtMagenta("%v:%v", getLocalIP(), port) + "\n"
	close := color.FmtItalic("%s", color.FmtGreen("Press CTRL+C to stop")) + "\n"

	_, _ = fmt.Fprint(os.Stdout, "\n"+accessURLs+divider+host+lan+divider+close)
}

func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, address := range addrs {
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return ""
}

func (app *App) markStopping() {
	app.serveMu.Lock()
	app.serveState = serveStopping
	app.serveMu.Unlock()
}

func drain(server *http.Server, timeout time.Duration) error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	err := server.Shutdown(shutdownCtx)
	cancel()
	if err == nil {
		return nil
	}
	_ = server.Close()

	return fmt.Errorf("core: shutdown timed out after %v", timeout)
}

func (app *App) runShutdownHooks() {
	app.shutdownOnce.Do(func() {
		app.serveMu.Lock()
		app.isRunningHooks = true
		app.serveMu.Unlock()

		if app.module != nil {
			for _, m := range app.module.collectModules() {
				if m.OnShutdown != nil {
					app.callShutdownHook(m)
				}
			}
		}

		app.serveMu.Lock()
		app.isRunningHooks = false
		app.serveState = serveStopped
		app.serveMu.Unlock()
		close(app.shutdownDone)
	})
}

func (app *App) callShutdownHook(m *Module) {
	defer func() {
		if rec := recover(); rec != nil {
			app.Logger.Error("OnShutdownPanic", "module", m.Name, "panic", rec)
		}
	}()

	m.OnShutdown()
}
