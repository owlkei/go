package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"go.owlky.com/owlky/bench"
)

func main() {
	framework := flag.String("framework", "owlky", "owlky, nethttp, gin, echo, chi or fiber")
	port := flag.Int("port", 8080, "port to listen on")
	isLogged := flag.Bool("log", false, "turn the framework's request logger on (JSON to stdout where the framework has one; net/http has none)")
	flag.Parse()

	go reportCPUOnSignal()

	addr := fmt.Sprintf(":%d", *port)
	var err error
	switch *framework {
	case "owlky":
		var l net.Listener
		if l, err = net.Listen("tcp", addr); err == nil {
			err = pick(*isLogged, bench.NewOwlkyApp, bench.NewOwlkyAppWithLog)().Serve(l)
		}
	case "nethttp":
		err = http.ListenAndServe(addr, bench.NewNetHTTP())
	case "gin":
		err = http.ListenAndServe(addr, pick(*isLogged, bench.NewGin, bench.NewGinWithLog)())
	case "echo":
		err = http.ListenAndServe(addr, pick(*isLogged, bench.NewEcho, bench.NewEchoWithLog)())
	case "chi":
		err = http.ListenAndServe(addr, pick(*isLogged, bench.NewChi, bench.NewChiWithLog)())
	case "fiber":
		err = pick(*isLogged, bench.NewFiber, bench.NewFiberWithLog)().Listen(addr)
	default:
		err = fmt.Errorf("unknown framework %q", *framework)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func pick[T any](isLogged bool, off, on T) T {
	if isLogged {
		return on
	}
	return off
}

func reportCPUOnSignal() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGUSR1)
	for range signals {
		var usage syscall.Rusage
		if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
			fmt.Fprintln(os.Stderr, "cpu error", err)
			continue
		}
		fmt.Fprintln(os.Stderr, "cpu_us", usage.Utime.Nano()/1000+usage.Stime.Nano()/1000)
	}
}
