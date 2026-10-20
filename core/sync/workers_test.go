package sync_test

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	vc "klefki/core/crypto"
	vs "klefki/core/sync"
	"klefki/core/syncwire"
)

func TestWorkerFailedFileDoesNotBlockNewDocument(t *testing.T) {
	h := setup(t)
	a, ca := h.device(t, "worker-source", true)
	b, cb := h.device(t, "worker-receiver", false)
	object, _ := vc.RandomID()
	old := add(t, a, h.master, object.String(), "", "old.txt", "old fixture")
	syncNow(t, ca)
	oldRecord, _ := a.Revision(h.master, old)
	var mu sync.Mutex
	var downloads []string
	cb.Remote, _ = vs.NewRemote(h.web.URL, &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "/chunks/") && r.Method == "GET" {
			mu.Lock()
			downloads = append(downloads, r.URL.Path)
			mu.Unlock()
			if strings.Contains(r.URL.Path, oldRecord.Metadata.FileID) {
				return &http.Response{StatusCode: 503, Header: http.Header{}, Body: http.NoBody}, nil
			}
		}
		return h.web.Client().Transport.RoundTrip(r)
	})}, false)
	if err := cb.Sync(context.Background()); err == nil {
		t.Fatal("injected file error was hidden")
	}
	object, _ = vc.RandomID()
	newest := add(t, a, h.master, object.String(), "", "scan_new.txt", "new fixture")
	syncNow(t, ca)
	_ = cb.Sync(context.Background())
	if got := content(t, b, h.master, newest); got != "new fixture" {
		t.Fatal("new document blocked by old failure")
	}
	mu.Lock()
	before := len(downloads)
	mu.Unlock()
	_ = cb.Sync(context.Background())
	mu.Lock()
	defer mu.Unlock()
	if len(downloads) != before {
		t.Fatal("failed job retried before its durable backoff elapsed")
	}
	copyPath := filepath.Join(h.root, "worker-receiver", "local.db")
	b.Close()
	reopened, err := vs.OpenExisting(copyPath, h.account.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	cb.Store = reopened
	needed, err := cb.NeedsSync(context.Background())
	if err != nil || needed {
		t.Fatal("restart ignored retry deadline", needed, err)
	}
}

func TestWorkerUnchangedAndMetadataOnlyAvoidBlobInventory(t *testing.T) {
	h := setup(t)
	a, ca := h.device(t, "worker-idle-a", true)
	_, cb := h.device(t, "worker-idle-b", false)
	object, _ := vc.RandomID()
	rev := add(t, a, h.master, object.String(), "", "same.txt", "unchanged bytes")
	if err := ca.SetMode(context.Background(), syncwire.TemporaryRelay); err != nil {
		t.Fatal(err)
	}
	syncNow(t, ca)
	syncNow(t, cb)
	syncNow(t, ca)
	syncNow(t, cb)
	var mu sync.Mutex
	calls := []string{}
	ca.Remote, _ = vs.NewRemote(h.web.URL, &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		calls = append(calls, r.URL.Path)
		mu.Unlock()
		return h.web.Client().Transport.RoundTrip(r)
	})}, false)
	syncNow(t, ca)
	for _, path := range calls {
		if strings.Contains(path, "/blobs/") || path == "/v1/sync/content/state" || path == "/v1/sync/relay/need-batch" {
			t.Fatal("unchanged pass inventoried old content", path)
		}
	}
	base, _ := a.Revision(h.master, rev)
	edit := vs.Edit{Name: base.Metadata.Name, Parent: base.Metadata.Parent, Tags: base.Metadata.Tags, Favorite: base.Metadata.Favorite}
	same, err := a.Edit(h.master, rev, edit)
	if err != nil || same != rev {
		t.Fatal("identical edit created a revision", same, err)
	}
	edit.Name = "renamed.txt"
	_, err = a.Edit(h.master, rev, edit)
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	calls = nil
	mu.Unlock()
	syncNow(t, ca)
	syncNow(t, cb)
	for _, path := range calls {
		if strings.Contains(path, "/blobs/") {
			t.Fatal("metadata edit retransferred old content", path)
		}
	}
}

func TestWorkerFailedUploadDoesNotBlockNewDocument(t *testing.T) {
	h := setup(t)
	a, ca := h.device(t, "worker-upload-source", true)
	b, cb := h.device(t, "worker-upload-receiver", false)
	syncNow(t, ca)
	syncNow(t, cb)
	object, _ := vc.RandomID()
	old := add(t, a, h.master, object.String(), "", "old-upload.txt", "old fixture")
	record, _ := a.Revision(h.master, old)
	ca.Remote, _ = vs.NewRemote(h.web.URL, &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method == "PUT" && strings.Contains(r.URL.Path, "/blobs/"+record.Metadata.FileID+"/chunks/") {
			return &http.Response{StatusCode: 503, Header: http.Header{}, Body: http.NoBody}, nil
		}
		return h.web.Client().Transport.RoundTrip(r)
	})}, false)
	if err := ca.Sync(context.Background()); err == nil {
		t.Fatal("injected upload error was hidden")
	}
	object, _ = vc.RandomID()
	newest := add(t, a, h.master, object.String(), "", "scan_new.txt", "new upload fixture")
	_ = ca.Sync(context.Background())
	_ = cb.Sync(context.Background())
	if got := content(t, b, h.master, newest); got != "new upload fixture" {
		t.Fatal("new document blocked by an older failed upload")
	}
}

func TestWorkerMetadataContinuesDuringStalledDownload(t *testing.T) {
	h := setup(t)
	a, ca := h.device(t, "worker-concurrent-a", true)
	b, cb := h.device(t, "worker-concurrent-b", false)
	object, _ := vc.RandomID()
	old := add(t, a, h.master, object.String(), "", "stalled.txt", "stalled fixture")
	syncNow(t, ca)
	oldRecord, _ := a.Revision(h.master, old)
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer close(release)
	cb.Remote, _ = vs.NewRemote(h.web.URL, &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method == "GET" && strings.Contains(r.URL.Path, oldRecord.Metadata.FileID+"/chunks/") {
			once.Do(func() { close(started) })
			select {
			case <-release:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
		}
		return h.web.Client().Transport.RoundTrip(r)
	})}, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- cb.Sync(ctx) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("download never started")
	}
	object, _ = vc.RandomID()
	newest := add(t, a, h.master, object.String(), "", "parallel.txt", "parallel fixture")
	syncNow(t, ca)
	deadline := time.Now().Add(9 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := b.Revision(h.master, newest); err == nil {
			cancel()
			<-done
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	err := <-done
	if !errors.Is(err, context.Canceled) {
		t.Log("sync result", err)
	}
	t.Fatal("metadata discovery waited for an unrelated file")
}

func TestWorkerContentBudgetSharedAcrossDocuments(t *testing.T) {
	h := setup(t)
	a, ca := h.device(t, "worker-budget", true)
	for _, name := range []string{"one.bin", "two.bin"} {
		object, _ := vc.RandomID()
		body := strings.Repeat("x", vc.ChunkSize*5)
		add(t, a, h.master, object.String(), "", name, body)
	}
	var mu sync.Mutex
	active, peak := 0, 0
	files := map[string]bool{}
	ca.Remote, _ = vs.NewRemote(h.web.URL, &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method == "PUT" && strings.Contains(r.URL.Path, "/chunks/") {
			mu.Lock()
			active++
			if active > peak {
				peak = active
			}
			files[strings.Split(r.URL.Path, "/")[3]] = true
			mu.Unlock()
			defer func() { mu.Lock(); active--; mu.Unlock() }()
			time.Sleep(10 * time.Millisecond)
		}
		return h.web.Client().Transport.RoundTrip(r)
	})}, false)
	syncNow(t, ca)
	if peak > 4 || peak < 2 || len(files) != 2 {
		t.Fatal("unexpected shared content budget", peak, len(files))
	}
}

func TestWorkerIncomingIsQuietOnBootstrapAndDurableAfterArrival(t *testing.T) {
	h := setup(t)
	a, ca := h.device(t, "worker-notice-a", true)
	b, cb := h.device(t, "worker-notice-b", false)
	object, _ := vc.RandomID()
	add(t, a, h.master, object.String(), "", "bootstrap.txt", "bootstrap fixture")
	syncNow(t, ca)
	syncNow(t, cb)
	if events, err := b.Incoming(h.master); err != nil || len(events) != 0 {
		t.Fatal("bootstrap announced existing documents", events, err)
	}
	object, _ = vc.RandomID()
	rev := add(t, a, h.master, object.String(), "", "incoming.txt", "incoming fixture")
	syncNow(t, ca)
	count := 0
	cb.OnDocument = func(event vs.DocumentEvent) {
		if event.Revision != rev || event.Stage != "ready" {
			t.Error("wrong incoming event", event)
		}
		count++
	}
	syncNow(t, cb)
	syncNow(t, cb)
	events, err := b.Incoming(h.master)
	if err != nil || len(events) != 1 || events[0].Read || count != 1 {
		t.Fatal("incoming event not durable/deduplicated", events, count, err)
	}
	if err = b.ReadIncoming(); err != nil {
		t.Fatal(err)
	}
	events, err = b.Incoming(h.master)
	if err != nil || len(events) != 1 || !events[0].Read {
		t.Fatal("read activity did not persist", events, err)
	}
}
