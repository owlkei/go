// Command feedback is a small public API set up the way it should run in production: load
// shedding, security headers, CORS, rate limits, an access log, a body size limit and a graceful
// shutdown.
package main

import (
	"log"
	"os"
	"time"

	"go.owlky.com/owlky"
	"go.owlky.com/owlky/guards/throttler"
)

func newApp(webOrigin string) owlky.App {
	// The protections run before routing, in a fixed order: load shedding first, so a request
	// refused under overload costs as little as possible.
	app := owlky.New(&owlky.AppOptions{
		IsAccessLog: true,
		Server:      &owlky.ServerOptions{ShutdownTimeout: 10 * time.Second},
		HTTP:        &owlky.HTTPOptions{MaxBodyBytes: 16 * owlky.KiB},
		Origins:     []string{webOrigin},
		LoadShed:    &owlky.LoadShedOptions{IsEnabled: true},
		Headers:     &owlky.HeadersOptions{IsEnabled: true},
		CORS:        &owlky.CORSOptions{IsEnabled: true},
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
