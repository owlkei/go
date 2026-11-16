package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"

	"github.com/dangduoc08/ginject/bench"
)

func main() {
	framework := flag.String("framework", "ginject", "ginject, nethttp, gin, echo, chi or fiber")
	port := flag.Int("port", 8080, "port to listen on")
	flag.Parse()

	addr := fmt.Sprintf(":%d", *port)
	var err error
	switch *framework {
	case "ginject":
		err = bench.NewGinjectApp().Listen(*port, nil)
	case "nethttp":
		err = http.ListenAndServe(addr, bench.NewNetHTTP())
	case "gin":
		err = http.ListenAndServe(addr, bench.NewGin())
	case "echo":
		err = http.ListenAndServe(addr, bench.NewEcho())
	case "chi":
		err = http.ListenAndServe(addr, bench.NewChi())
	case "fiber":
		err = bench.NewFiber().Listen(addr)
	default:
		err = fmt.Errorf("unknown framework %q", *framework)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
