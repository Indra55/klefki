package sync_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	vc "klefki/core/crypto"
	vs "klefki/core/sync"
)

func TestRelayPreparationRequestCounts(t *testing.T) {
	for _, count := range []int{0, 84, 256, 257, 769} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			h := setup(t)
			s, c := h.device(t, "batch", true)
			for i := 0; i < count; i++ {
				id, _ := vc.RandomID()
				if _, e := s.Put(h.master, id.String(), "", vc.Metadata{Type: "folder", Name: fmt.Sprint(i), Tags: []string{}}, nil); e != nil {
					t.Fatal(e)
				}
			}
			syncNow(t, c)
			transport := h.web.Client().Transport
			for _, legacy := range []int{0, 404, 405} {
				calls, chunks := 0, 0
				c.Remote, _ = vs.NewRemote(h.web.URL, &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
					calls++
					if strings.Contains(r.URL.Path, "/chunks/") {
						chunks++
					}
					if legacy != 0 && (strings.HasSuffix(r.URL.Path, "/prepare") || strings.HasSuffix(r.URL.Path, "/need-batch")) {
						return &http.Response{StatusCode: legacy, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
					}
					return transport.RoundTrip(r)
				})}, false)
				if legacy != 0 && count > 0 {
					if err := c.Sync(context.Background()); !errors.Is(err, vs.ErrContentUnsupported) {
						t.Fatal("mixed-version server did not require update", err)
					}
					if chunks != 0 {
						t.Fatal("unsupported sync transferred chunks")
					}
					continue
				}
				syncNow(t, c)
				expected := 7
				if count > 0 {
					expected += (count + 255) / 256
				}
				if legacy != 0 {
					expected = 9
				}
				if calls != expected || chunks != 0 {
					t.Fatalf("legacy=%d requests=%d want=%d chunks=%d", legacy, calls, expected, chunks)
				}
			}
			if count == 84 {
				calls := 0
				c.Remote, _ = vs.NewRemote(h.web.URL, &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
					calls++
					time.Sleep(300 * time.Millisecond)
					return transport.RoundTrip(r)
				})}, false)
				start := time.Now()
				if e := c.Sync(context.Background()); e != nil {
					t.Fatal(e)
				}
				t.Logf("84 revisions, 300ms/request: %d requests, production sync %s", calls, time.Since(start))
			}
		})
	}
}
