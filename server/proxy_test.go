package server

import (
	"net/http/httptest"
	"testing"
)

func TestProxyTransportIsExplicitAndRequiresHTTPS(t *testing.T) {
	for _, trust := range []bool{false, true} {
		s, e := Open(Config{DataDir: t.TempDir(), TrustProxy: trust})
		if e != nil {
			t.Fatal(e)
		}
		for _, proto := range []string{"", "http", "https", "https,http"} {
			r := httptest.NewRequest("GET", "http://origin/v1/accounts/11111111111111111111111111111111", nil)
			r.Header.Set("X-Forwarded-Proto", proto)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			want := 426
			if trust && proto == "https" {
				want = 404
			}
			if w.Code != want {
				t.Fatalf("trust=%v proto=%q: %d", trust, proto, w.Code)
			}
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("GET", "http://origin/healthz", nil))
		if w.Code != 200 {
			t.Fatal("platform health check requires proxy headers")
		}
		s.Close()
	}
}
