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
	"go.owlky.com/owlky/middlewares/cors"
	"go.owlky.com/owlky/middlewares/helmet"
	"go.owlky.com/owlky/middlewares/loadshed"
)

var serverOptions = &owlky.ServerOptions{
	ShutdownTimeout:     10 * time.Second,
	MaxRequestBodyBytes: 16 << 10,
}

func newApp(webOrigin string) owlky.App {
	app := owlky.New()
	// loadshed comes first: a request refused under overload should cost as little as possible.
	app.BindGlobalMiddlewares(
		loadshed.LoadShed{},
		helmet.Helmet{},
		cors.CORS{AllowOrigins: []string{webOrigin}},
	)
	app.BindGlobalGuards(throttler.Throttler{Limit: 100, TTL: time.Minute})
	app.EnableAccessLog()
	app.Create(NewFeedbackModule())
	return app
}

func main() {
	origin := os.Getenv("WEB_ORIGIN")
	if origin == "" {
		origin = "http://localhost:5173"
	}
	if err := newApp(origin).Listen(3000, serverOptions); err != nil {
		log.Fatal(err)
	}
}
