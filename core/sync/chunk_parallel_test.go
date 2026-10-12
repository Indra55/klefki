package sync_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	vc "klefki/core/crypto"
	vs "klefki/core/sync"
)

func TestFourChunkTransfersPreserveContent(t *testing.T) {
	h := setup(t)
	a, ca := h.device(t, "parallel-a", true)
	b, cb := h.device(t, "parallel-b", false)
	object, _ := vc.RandomID()
	data := bytes.Repeat([]byte("parallel authenticated fixture "), 600000)
	revision := add(t, a, h.master, object.String(), "", "parallel.bin", string(data))
	for _, client := range []*vs.Client{ca, cb} {
		var active, peak atomic.Int32
		transport := h.web.Client().Transport
		client.Remote, _ = vs.NewRemote(h.web.URL, &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			if strings.Contains(r.URL.Path, "/chunks/") {
				n := active.Add(1)
				defer active.Add(-1)
				for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
				}
				select {
				case <-time.After(30 * time.Millisecond):
				case <-r.Context().Done():
					return nil, r.Context().Err()
				}
			}
			return transport.RoundTrip(r)
		})}, false)
		if e := client.Sync(context.Background()); e != nil {
			t.Fatal(e)
		}
		if peak.Load() != 4 || active.Load() != 0 {
			t.Fatalf("peak=%d active=%d", peak.Load(), active.Load())
		}
	}
	var received bytes.Buffer
	if e := b.Export(h.master, revision, &received); e != nil {
		t.Fatal(e)
	}
	if sha256.Sum256(data) != sha256.Sum256(received.Bytes()) {
		t.Fatal("received hash differs")
	}
}
