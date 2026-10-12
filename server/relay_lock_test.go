package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type gatedBody struct {
	started chan struct{}
	release chan struct{}
}

func (b *gatedBody) Read([]byte) (int, error) { close(b.started); <-b.release; return 0, io.EOF }
func (b *gatedBody) Close() error             { return nil }

type gatedWriter struct {
	*httptest.ResponseRecorder
	started chan struct{}
	release chan struct{}
}

func (w *gatedWriter) Write(b []byte) (int, error) {
	close(w.started)
	<-w.release
	return w.ResponseRecorder.Write(b)
}
func TestRelayNetworkOutsideAccountLock(t *testing.T) {
	s := &Server{}
	request := func(account string) *http.Request {
		r := httptest.NewRequest("PUT", "/v1/sync/relay/prepare", nil)
		return r.WithContext(context.WithValue(r.Context(), identityKey{}, identity{account: account}))
	}
	handler := s.relayBuffered(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }))
	body := &gatedBody{make(chan struct{}), make(chan struct{})}
	r := request("a")
	r.Body = body
	done := make(chan struct{})
	go func() { handler.ServeHTTP(httptest.NewRecorder(), r); close(done) }()
	<-body.started
	assertAvailable := func() {
		t.Helper()
		finished := make(chan struct{})
		go func() {
			for _, account := range []string{"a", "b"} {
				handler.ServeHTTP(httptest.NewRecorder(), request(account))
			}
			close(finished)
		}()
		select {
		case <-finished:
		case <-time.After(time.Second):
			t.Error("network I/O held account lock")
		}
	}
	assertAvailable()
	close(body.release)
	<-done
	done = make(chan struct{})
	writer := &gatedWriter{httptest.NewRecorder(), make(chan struct{}), make(chan struct{})}
	go func() { handler.ServeHTTP(writer, request("a")); close(done) }()

	<-writer.started
	assertAvailable()
	close(writer.release)
	<-done
}
func TestAccountLockRegistryReleased(t *testing.T) {
	var locks accountLocks
	var wg sync.WaitGroup
	value := 0
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 30; j++ {
				unlock := locks.lock("a")
				value++
				unlock()
			}
		}()
	}
	wg.Wait()
	if value != 960 || len(locks.entries) != 0 {
		t.Fatalf("value=%d retained=%d", value, len(locks.entries))
	}
}
