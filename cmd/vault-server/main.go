package main

import (
	"context"
	"flag"
	"io"
	stdlog "log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	"vault/server"
)

func main() { os.Exit(run()) }
func run() int {
	addr := flag.String("listen", "127.0.0.1:8080", "listen address")
	data := flag.String("data", "./server-data", "private data directory")
	cert := flag.String("tls-cert", "", "TLS certificate path")
	key := flag.String("tls-key", "", "TLS key path")
	dev := flag.Bool("dev-http", false, "allow HTTP on a loopback listener only")
	flag.Parse()
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if *dev {
		host, _, e := net.SplitHostPort(*addr)
		ip := net.ParseIP(host)
		if e != nil || ip == nil || !ip.IsLoopback() {
			log.Error("dev HTTP requires a literal loopback address")
			return 1
		}
	} else if *cert == "" || *key == "" {
		log.Error("TLS certificate and key required")
		return 1
	}
	s, e := server.Open(server.Config{DataDir: *data, AllowHTTP: *dev, Logger: log})
	if e != nil {
		log.Error("server initialization failed")
		return 1
	}
	defer s.Close()
	var handler http.Handler = s

	h := &http.Server{Addr: *addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 8192}
	// net/http's default error logger may include peer-controlled TLS details.
	h.ErrorLog = stdlog.New(io.Discard, "", 0)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() {
		if *dev {
			done <- h.ListenAndServe()
		} else {
			done <- h.ListenAndServeTLS(*cert, *key)
		}
	}()
	select {
	case e := <-done:
		if e != http.ErrServerClosed {
			log.Error("listener failed")
			return 1
		}
	case <-ctx.Done():
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if h.Shutdown(ctx) != nil {
			h.Close()
		}
	}
	return 0
}
