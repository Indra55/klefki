package server

import (
	"net/http"
	"testing"
	"time"
)

func TestLibraryRateLimitDoesNotConsumeDocumentBudget(t *testing.T) {
	now := time.Now()
	s, err := Open(Config{DataDir: t.TempDir(), RateBurst: 1, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	check := func(method, path string, code int, scope string) {
		t.Helper()
		w := statusRequest(s, "", "", method, path, nil)
		if w.Code != code || w.Header().Get("X-Klefki-Retry-Scope") != scope {
			t.Fatalf("%s %s: status %d, scope %q", method, path, w.Code, w.Header().Get("X-Klefki-Retry-Scope"))
		}
	}
	check(http.MethodGet, "/v1/sync/check", http.StatusUnauthorized, "")
	check(http.MethodGet, "/v1/sync/changes?after=0", http.StatusTooManyRequests, "library")
	check(http.MethodPut, "/v1/sync/metadata-cursor", http.StatusTooManyRequests, "library")
	check(http.MethodPut, "/v1/sync/revisions/11111111111111111111111111111111", http.StatusUnauthorized, "")
	check(http.MethodGet, "/v1/blobs/11111111111111111111111111111111/chunks/0", http.StatusTooManyRequests, "data")
	check(http.MethodGet, "/v1/sync/status", http.StatusUnauthorized, "")
	check(http.MethodPost, "/v1/sync/presence", http.StatusTooManyRequests, "status")
	now = now.Add(time.Minute)
	check(http.MethodGet, "/v1/sync/check", http.StatusUnauthorized, "")
	check(http.MethodPut, "/v1/sync/revisions/11111111111111111111111111111111", http.StatusUnauthorized, "")
}
