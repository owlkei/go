// Command feedback is a small public API set up the way it should run in production: load
// shedding, security headers, CORS, rate limits, an access log, a body size limit and a graceful
// shutdown.
package main

import (
	"log"
	"os"
	"time"

	"go.owlkei.com/owlkei"
	"go.owlkei.com/owlkei/guards/throttler"
)

func newApp(webOrigin string) owlkei.App {
	// The protections run before routing, in a fixed order: load shedding first, so a request
	// refused under overload costs as little as possible.
	app := owlkei.New(&owlkei.AppOptions{
		IsAccessLog: true,
		Server:      &owlkei.ServerOptions{ShutdownTimeout: 10 * time.Second},
		HTTP:        &owlkei.HTTPOptions{MaxBodyBytes: 16 * owlkei.KiB},
		Origins:     []string{webOrigin},
		LoadShed:    &owlkei.LoadShedOptions{IsEnabled: true},
		Headers:     &owlkei.HeadersOptions{IsEnabled: true},
		CORS:        &owlkei.CORSOptions{IsEnabled: true},
	})
	app.BindGlobalGuards(throttler.Throttler{Limit: 100, TTL: time.Minute})
	app.Create(NewFeedbackModule())
	return app
}

func main() {
	origin := os.Getenv("WEB_ORIGIN")
	if origin == "" {
		origin = "http://localhost:5173"
	}
	if err := newApp(origin).Listen(); err != nil {
		log.Fatal(err)
	}
}
