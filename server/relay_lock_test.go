package server

import (
	"bytes"
	"context"
	"io"
	vc "klefki/core/crypto"
	"klefki/core/syncwire"
	"net/http"
	"net/http/httptest"
	"os"
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

func TestRelayCleanupDuringDownload(t *testing.T) {
	s, e := Open(Config{DataDir: t.TempDir(), RateBurst: 10000})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ts := httptest.NewTLSServer(s)
	defer ts.Close()
	f := newFixture(t)
	session := enroll(t, ts.Client(), ts.URL, f)
	first, _ := vc.RandomID()
	second, _ := vc.RandomID()
	file, _ := vc.RandomID()
	revision, _ := vc.RandomID()
	key, _ := vc.RandomKey()
	chunk, e := vc.Seal(key, vc.Context{Kind: vc.Chunk, Account: f.account.ID, Object: file, Final: true}, []byte("download survives cleanup"), nil)
	if e != nil {
		t.Fatal(e)
	}
	account := f.account.ID.String()
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO relay_settings VALUES(?,?)`, []any{account, syncwire.TemporaryRelay}},
		{`INSERT INTO relay_devices VALUES(?,?,1),(?,?,1)`, []any{account, first.String(), account, second.String()}},
		{`INSERT INTO changes(sequence,account,revision,object,base,metadata) VALUES(1,?,?,?,'',X'01')`, []any{account, revision.String(), revision.String()}},
		{`INSERT INTO relay_revisions VALUES(?,?,X'01',?)`, []any{account, revision.String(), file.String()}},
	}
	for _, statement := range statements {
		if _, e := s.db.Exec(statement.query, statement.args...); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := s.blobs.Put(account, file.String(), 0, chunk); e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("GET", "https://example.test/v1/blobs/"+file.String()+"/chunks/0", nil)
	r.Header.Set("Authorization", "Bearer "+session.Token)
	r.Header.Set("X-Klefki-Device", first.String())
	writer := &gatedWriter{httptest.NewRecorder(), make(chan struct{}), make(chan struct{})}
	finished := make(chan struct{})
	go func() { s.ServeHTTP(writer, r); close(finished) }()
	<-writer.started
	done := make(chan error, 1)
	go func() { unlock := s.relayLocks.lock(account); defer unlock(); done <- s.cleanupRelay(account) }()
	select {
	case e := <-done:
		if e != nil {
			t.Error(e)
		}
	case <-time.After(time.Second):
		t.Error("cleanup blocked on network response")
	}
	close(writer.release)
	<-finished
	if writer.Code != 200 || !bytes.Equal(writer.Body.Bytes(), chunk) {
		t.Fatal("cleanup changed copied download")
	}
	if _, e := s.blobs.Get(account, file.String(), 0); !os.IsNotExist(e) {
		t.Fatalf("cleanup left blob: %v", e)
	}
}
