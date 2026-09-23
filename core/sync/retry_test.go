package sync

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestBackoffAndPermanentErrors(t *testing.T) {
	calls := 0
	remote, e := NewRemote("https://example.invalid", &http.Client{Transport: testTransport(func(*http.Request) (*http.Response, error) { calls++; return nil, io.ErrUnexpectedEOF })}, false)
	if e != nil {
		t.Fatal(e)
	}
	client := Client{Remote: remote, Retry: Retry{Attempts: 3, Base: 10 * time.Millisecond, Max: 20 * time.Millisecond}}
	start := time.Now()
	if _, e = client.request(context.Background(), "GET", "/v1/sync/cursor", nil, 1024); e == nil || calls != 3 || time.Since(start) < 30*time.Millisecond {
		t.Fatal("exponential backoff missing")
	}
	calls = 0
	remote.client.Transport = testTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 401, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("unauthorized"))}, nil
	})
	if _, e = client.request(context.Background(), "GET", "/v1/sync/cursor", nil, 1024); e == nil || calls != 1 {
		t.Fatal("permanent auth error retried")
	}
}
func TestCancelDuringRetryAfter(t *testing.T) {
	calls := 0
	remote, e := NewRemote("https://example.invalid", &http.Client{Transport: testTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{"120"}}, Body: io.NopCloser(strings.NewReader("limited"))}, nil
	})}, false)
	if e != nil {
		t.Fatal(e)
	}
	client := Client{Remote: remote, Retry: Retry{Attempts: 3, Base: time.Millisecond}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, e = client.request(ctx, "GET", "/v1/sync/cursor", nil, 1024); e != context.DeadlineExceeded || calls != 1 {
		t.Fatal("retry-after was ignored or uncancellable")
	}
}
