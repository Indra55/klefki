package main

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"klefki/server"
)

func main() {
	data := flag.String("data", "", "isolated benchmark data directory")
	web := flag.String("web", "", "built web directory")
	burst := flag.Int("rate-burst", 12000, "local request burst allowance")
	flag.Parse()
	s, err := server.Open(server.Config{DataDir: *data, AllowHTTP: true, RateBurst: *burst})
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()
	handler, err := server.WebHandler(s, *web)
	if err != nil {
		log.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	h := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: time.Minute, MaxHeaderBytes: 8192, ErrorLog: log.New(io.Discard, "", 0)}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- h.Serve(listener) }()
	json.NewEncoder(os.Stdout).Encode(map[string]string{"url": "http://" + listener.Addr().String()})
	select {
	case err := <-done:
		if err != http.ErrServerClosed {
			log.Print(err)
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		h.Shutdown(shutdown)
	}
}
