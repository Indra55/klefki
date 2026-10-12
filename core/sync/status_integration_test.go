package sync_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	vc "klefki/core/crypto"
	vs "klefki/core/sync"
	"klefki/core/syncwire"
)

func TestProgressReceiptsAndSourceSurviveRepublish(t *testing.T) {
	h := setup(t)
	a, clientA := h.device(t, "status-a", true)
	b, clientB := h.device(t, "status-b", false)
	localA, _ := a.RelayState()
	localB, _ := b.RelayState()
	data := bytes.Repeat([]byte{42}, vc.ChunkSize+19)
	object, _ := vc.RandomID()
	id, err := a.Put(h.master, object.String(), "", vc.Metadata{Type: "file", Name: "progress.bin", Tags: []string{}, Size: uint64(len(data))}, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if origin, err := a.Origin(id); err != nil || origin != localA.Device {
		t.Fatal("local origin missing", origin, err)
	}
	var uploaded, downloaded []syncwire.Progress
	clientA.Observe = func(p syncwire.Progress) { uploaded = append(uploaded, p) }
	clientB.Observe = func(p syncwire.Progress) { downloaded = append(downloaded, p) }
	syncNow(t, clientA)
	if err = clientA.Remote.Presence(context.Background(), clientA.Session, syncwire.Presence{Device: localA.Device, ClientKind: "desktop", Phase: "idle"}); err != nil {
		t.Fatal(err)
	}
	syncNow(t, clientB)
	if err = clientB.Remote.Presence(context.Background(), clientB.Session, syncwire.Presence{Device: localB.Device, ClientKind: "web", Phase: "idle"}); err != nil {
		t.Fatal(err)
	}
	if origin, err := b.Origin(id); err != nil || origin != localA.Device {
		t.Fatal("import lost source", origin, err)
	}
	var sawUpload, sawDownload bool
	for _, p := range uploaded {
		if p.Direction == "upload" && p.Phase == "uploading" && p.Chunk == 1 && p.DoneBytes == vc.ChunkSize {
			sawUpload = true
		}
		if p.DoneBytes > p.TotalBytes || p.ReusedBytes > p.DoneBytes {
			t.Fatal("inflated upload counters", p)
		}
	}
	for _, p := range downloaded {
		if p.Direction == "download" && p.Phase == "complete" && p.Source == localA.Device && p.DoneBytes == uint64(len(data)) {
			sawDownload = true
		}
		if p.DoneBytes > p.TotalBytes || p.ReusedBytes > p.DoneBytes {
			t.Fatal("inflated download counters", p)
		}
	}
	if !sawUpload || !sawDownload {
		t.Fatal("missing confirmed transfer progress", sawUpload, sawDownload)
	}
	peers, err := clientA.Remote.Status(context.Background(), clientA.Session)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range peers.Devices {
		if d.Outstanding != 0 {
			t.Fatal("receipt not durable", d)
		}
	}
	if err = clientA.SetMode(context.Background(), syncwire.TemporaryRelay); err != nil {
		t.Fatal(err)
	}
	syncNow(t, clientB)
	h.web.Close()
	h.backend.Close()
	h.start(t)
	syncNow(t, clientB)
	if origin, _ := b.Origin(id); origin != localA.Device {
		t.Fatal("relay reseed changed origin")
	}
}

func TestFailedPublicationDoesNotReportCompletion(t *testing.T) {
	h := setup(t)
	s, client := h.device(t, "status-failure", true)
	object, _ := vc.RandomID()
	id := add(t, s, h.master, object.String(), "", "pending.txt", "durable bytes")
	web := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/sync/revisions/") {
			http.Error(w, "full", 507)
			return
		}
		h.backend.ServeHTTP(w, r)
	}))
	defer web.Close()
	remote, err := vs.NewRemote(web.URL, web.Client(), false)
	if err != nil {
		t.Fatal(err)
	}
	client.Remote = remote
	client.Observe = func(p syncwire.Progress) {
		if p.Revision == id && p.Phase == "complete" {
			t.Error("publication failure reported complete")
		}
	}
	if err = client.Sync(context.Background()); err == nil {
		t.Fatal("failure swallowed")
	}
	if n, err := s.Pending(); err != nil || n != 1 {
		t.Fatal("pending work lost", n, err)
	}
}
