package core

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	stdlog "log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"go.owlky.com/owlky/internal/color"
	"go.owlky.com/owlky/internal/logpipe"
	"go.owlky.com/owlky/internal/routing"
)

const defaultShutdownTimeout = 30 * time.Second

var (
	errNotCreated     = errors.New("owlky: Create must be called before serving")
	errAlreadyServing = errors.New("owlky: app is already serving")
	errStopped        = errors.New("owlky: app is stopped")
)

type serverConfig struct {
	port            int
	configuredPort  int
	shutdownTimeout time.Duration
	tlsConfig       *tls.Config
	isH2C           bool
}

func (cfg *appConfig) serverConfig(opts *AppOptions) serverConfig {
	s := serverConfig{port: cfg.port, shutdownTimeout: cfg.shutdownTimeout, tlsConfig: cfg.tlsConfig, isH2C: cfg.isH2C}
	if opts != nil && opts.Server != nil {
		s.configuredPort = opts.Server.Port
	}
	return s
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

func (app *App) Listen() error {
	if err := app.servingError(); err != nil {
		return err
	}

	return app.listenAndServe(app.server.port)
}

func (app *App) Serve(l net.Listener) error {
	return app.serve(l, true)
}

func (app *App) Stop() {
	app.serveMu.Lock()
	switch app.serveState {
	case serveCreated:
		app.serveState = serveStopped
		app.serveMu.Unlock()
		app.runShutdownHooks(time.Now().Add(app.server.shutdownTimeout))
		return
	case serveServing:
		app.serveState = serveStopping
		close(app.stopRequest)
	}
	hooksGoroutine := app.hooksGoroutine
	app.serveMu.Unlock()

	self := goroutineID()
	if (hooksGoroutine != 0 && hooksGoroutine == self) || app.container.isEntryGoroutine(self) {
		return
	}
	<-app.shutdownDone
}

func goroutineID() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	header := bytes.TrimPrefix(buf[:n], []byte("goroutine "))
	end := bytes.IndexByte(header, ' ')
	if end < 0 {
		return 0
	}
	id, err := strconv.ParseUint(string(header[:end]), 10, 64)
	if err != nil {
		return 0
	}

	return id
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

func (app *App) servingError() error {
	app.serveMu.Lock()
	defer app.serveMu.Unlock()

	return app.servingErrorLocked()
}

func (app *App) servingErrorLocked() error {
	switch {
	case !app.isCreated:
		return errNotCreated
	case app.serveState == serveServing || app.serveState == serveStopping:
		return errAlreadyServing
	case app.serveState == serveStopped:
		return errStopped
	}

	return nil
}

func (app *App) listenAndServe(port int) error {
	address := net.JoinHostPort("", strconv.Itoa(port))
	l, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("owlky: listen on %s: %w", address, err)
	}
	defer func() { _ = l.Close() }()

	return app.serve(l, false)
}

func (app *App) serve(l net.Listener, isOwnListener bool) error {
	stopRequest, err := app.beginServing()
	if err != nil {
		return err
	}

	if isOwnListener && app.server.configuredPort != 0 {
		app.logger.Info("ServerPortIgnored", "port", app.server.configuredPort)
	}
	timeout := app.server.shutdownTimeout
	isTLS := app.server.tlsConfig != nil
	server := app.newServer(isTLS)
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

	var deadline time.Time
	select {
	case sig := <-signals:
		stopSignals()
		deadline = app.beginShutdown(timeout)
		app.markStopping()
		app.logger.Info("ShutdownSignal", "signal", sig.String())
		err = drain(server, timeout)
		<-served
	case <-stopRequest:
		stopSignals()
		deadline = app.beginShutdown(timeout)
		app.logger.Info("Shutdown")
		err = drain(server, timeout)
		<-served
	case <-served:
		stopSignals()
		deadline = app.beginShutdown(timeout)
		app.markStopping()
		_ = drain(server, timeout)
		err = fmt.Errorf("owlky: serve: %w", serveErr)
	}

	app.runShutdownHooks(deadline)

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

func (app *App) newServer(isTLS bool) *http.Server {
	var protocols http.Protocols
	protocols.SetHTTP1(true)
	switch {
	case isTLS:
		protocols.SetHTTP2(allowsHTTP2(app.server.tlsConfig))
	case app.server.isH2C:
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
		TLSConfig:         app.server.tlsConfig,
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
	w.app.logger.Warn("HTTPServer", "message", strings.TrimSuffix(string(p), "\n"))

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

func (app *App) beginShutdown(timeout time.Duration) time.Time {
	deadline := time.Now().Add(timeout)
	app.beginAsyncShutdown()
	return deadline
}

func (app *App) logStartup(addr net.Addr, isTLS bool, protocols string) {
	app.container.mu.RLock()
	modules := slices.Clone(app.container.modules)
	app.container.mu.RUnlock()
	for _, m := range modules {
		if m.Name != "" && app.container.status(m) == moduleReady {
			app.logger.Info("InstanceLoader", "module", m.Name+" initialized")
		}
	}

	patterns := make([]string, 0, len(app.container.httpMainHandlers))
	for _, h := range app.container.httpMainHandlers {
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
		app.logger.Info("RouteExplorer", args...)
	}

	if app.isWSEnabled {
		for _, h := range app.container.wsMainHandlers {
			app.logger.Info("WebSocketEvent", "event", h.EventName)
		}
	}

	if tcpAddr, ok := addr.(*net.TCPAddr); ok && isTerminal(os.Stdout) {
		logpipe.Stdout().Flush(logpipe.FlushTimeout)
		logBoostrap(tcpAddr.Port, isTLS)
		return
	}
	app.logger.Info("ServerListening", "address", addr.String(), "tls", isTLS, "protocols", protocols)
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()

	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func logBoostrap(port int, isTLS bool) {
	scheme := "http"
	if isTLS {
		scheme = "https"
	}
	accessURLs := color.FmtBold("%s", color.FmtBGYellow(" GG! Here Are Your Access URLs: ")) + "\n"
	divider := color.FmtDim("--------------------------------------------") + "\n"
	host := color.FmtBold("%s", color.FmtWhite("Localhost: ")) + color.FmtMagenta("%s://localhost:%v", scheme, port) + "\n"
	lan := color.FmtBold("%s", color.FmtWhite("      LAN: ")) + color.FmtMagenta("%s://%v:%v", scheme, getLocalIP(), port) + "\n"
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

	return fmt.Errorf("owlky: shutdown timed out after %v", timeout)
}

func (app *App) runShutdownHooks(deadline time.Time) {
	app.shutdownOnce.Do(func() {
		app.serveMu.Lock()
		app.hooksGoroutine = goroutineID()
		isCreated := app.isCreated
		app.serveMu.Unlock()

		app.beginAsyncShutdown()
		app.waitEntries(deadline)
		if isCreated {
			app.runModuleShutdownHooks()
		}
		if app.accesslog != nil {
			app.accesslog.Stop()
		}
		app.stacks.shutdown()

		app.serveMu.Lock()
		isLogAsync := app.isLogAsync
		app.isLogAsync = false
		app.serveMu.Unlock()
		if isLogAsync {
			logpipe.Stdout().StopAsync(logpipe.FlushTimeout)
		}

		app.serveMu.Lock()
		app.hooksGoroutine = 0
		app.serveState = serveStopped
		app.serveMu.Unlock()
		close(app.shutdownDone)
	})
}

func (app *App) runModuleShutdownHooks() {
	app.container.mu.RLock()
	modules := slices.Clone(app.container.readyOrder)
	app.container.mu.RUnlock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		app.serveMu.Lock()
		app.hooksGoroutine = goroutineID()
		app.serveMu.Unlock()
		for i := len(modules) - 1; i >= 0; i-- {
			if modules[i].OnShutdown != nil {
				app.callShutdownHook(modules[i])
			}
		}
	}()
	timer := time.NewTimer(app.server.shutdownTimeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		app.logger.Warn("ShutdownHooksTimeout", "timeout", app.server.shutdownTimeout.String())
	}
}

func (app *App) callShutdownHook(m *Module) {
	defer func() {
		if rec := recover(); rec != nil {
			app.logger.Error("OnShutdownPanic", "module", m.Name, "panic", rec)
		}
	}()

	m.OnShutdown()
}
