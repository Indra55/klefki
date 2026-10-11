// Loopback browser-test server. Production limits are tested in server tests.
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"klefki/server"
)

func main() {
	if len(os.Args) != 3 {
		panic("test server arguments")
	}
	s, e := server.Open(server.Config{DataDir: os.Args[1], AllowHTTP: true, RateBurst: 10000})
	if e != nil {
		panic("test server initialization failed")
	}
	defer s.Close()
	handler, e := server.WebHandler(s, os.Args[2])
	if e != nil {
		panic("test web build missing")
	}
	h := &http.Server{Addr: "127.0.0.1:5188", Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: time.Minute, MaxHeaderBytes: 8192}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- h.ListenAndServe() }()
	select {
	case e := <-done:
		if e != http.ErrServerClosed {
			panic("test listener failed")
		}
	case <-ctx.Done():
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if h.Shutdown(ctx) != nil {
			h.Close()
		}
	}
}
