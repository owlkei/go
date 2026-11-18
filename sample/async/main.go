package main

import (
	"os"
	"strconv"

	"github.com/dangduoc08/ginject"
)

const defaultPort = 8081

func main() {
	port := defaultPort
	if v := os.Getenv("PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			panic("PORT must be a number: " + v)
		}
		port = p
	}

	app := ginject.New()
	app.Create(Module)
	if err := app.Listen(port, nil); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
