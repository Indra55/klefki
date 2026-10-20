package sync_test

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	vc "klefki/core/crypto"
	vs "klefki/core/sync"
	"klefki/core/syncwire"
)

func TestLocalDocumentsRemainAccessibleDuringDownload(t *testing.T) {
	h := setup(t)
	a, ca := h.device(t, "responsive-a", true)
	b, cb := h.device(t, "responsive-b", false)
	obj, _ := vc.RandomID()
	original := add(t, a, h.master, obj.String(), "", "available.txt", "available offline")
	syncNow(t, ca)
	syncNow(t, cb)
	obj, _ = vc.RandomID()
	add(t, a, h.master, obj.String(), "", "incoming.txt", "incoming bytes")
	syncNow(t, ca)
	started, resume := make(chan struct{}), make(chan struct{})
	transport := h.web.Client().Transport
	cb.Remote, _ = vs.NewRemote(h.web.URL, &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method == "GET" && strings.Contains(r.URL.Path, "/chunks/") {
			close(started)
			select {
			case <-resume:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
		}
		return transport.RoundTrip(r)
	})}, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- cb.Sync(ctx) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("download did not start")
	}
	read := make(chan error, 1)
	go func() {
		heads, e := b.Heads(h.master)
		if e == nil && (len(heads) != 1 || heads[0].Revision != original) {
			e = vs.ErrInvalid
		}
		if e == nil {
			var out bytes.Buffer
			e = b.Export(h.master, original, &out)
			if e == nil && out.String() != "available offline" {
				e = vs.ErrInvalid
			}
		}
		if e == nil {
			id, _ := vc.RandomID()
			_, e = b.Put(h.master, id.String(), "", vc.Metadata{Type: "folder", Name: "Local edit", Tags: []string{}}, nil)
		}
		read <- e
	}()
	select {
	case e := <-read:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		cancel()
		<-finished
		<-read
		t.Fatal("download held local storage hostage")
	}
	close(resume)
	if e := <-finished; e != nil {
		t.Fatal(e)
	}
	if heads, err := b.Heads(h.master); err != nil || len(heads) != 3 {
		t.Fatal("concurrent local edit was lost", err)
	}
	syncNow(t, cb)
}

func TestIdlePollDetectsEditsAndLateJoin(t *testing.T) {
	h := setup(t)
	a, ca := h.device(t, "poll-a", true)
	_, cb := h.device(t, "poll-b", false)
	ctx := context.Background()
	if e := ca.SetMode(ctx, syncwire.TemporaryRelay); e != nil {
		t.Fatal(e)
	}
	obj, _ := vc.RandomID()
	add(t, a, h.master, obj.String(), "", "poll.txt", "poll bytes")
	syncNow(t, ca)
	syncNow(t, cb)
	syncNow(t, ca)
	if needed, e := ca.NeedsSync(ctx); e != nil || needed {
		t.Fatalf("idle poll: %v %v", needed, e)
	}
	if needed, e := cb.NeedsSync(ctx); e != nil || needed {
		t.Fatalf("idle recipient: %v %v", needed, e)
	}
	_, late := h.device(t, "poll-late", false)
	if e := late.Sync(ctx); e != vs.ErrRelayWaiting {
		t.Fatalf("late join: %v", e)
	}
	if needed, e := ca.NeedsSync(ctx); e != nil || !needed {
		t.Fatalf("missed late join: %v %v", needed, e)
	}
	syncNow(t, ca)
	syncNow(t, late)
	syncNow(t, ca)
	obj, _ = vc.RandomID()
	add(t, a, h.master, obj.String(), "", "edit.txt", "local change")
	if needed, e := ca.NeedsSync(ctx); e != nil || !needed {
		t.Fatalf("missed local edit: %v %v", needed, e)
	}
}
