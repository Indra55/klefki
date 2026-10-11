package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWebHandler(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{"index.html": "<html>Klefki</html>", "sw.js": "// worker", "manifest.webmanifest": "{}", "font.woff2": "font", "pdf.worker.mjs": "// PDF worker"} {
		if e := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); e != nil {
			t.Fatal(e)
		}
	}
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) })
	h, e := WebHandler(api, dir)
	if e != nil {
		t.Fatal(e)
	}
	for _, tt := range []struct {
		path   string
		status int
	}{{"/", 200}, {"/sw.js", 200}, {"/v1/sync/changes", 401}, {"/missing", 404}, {"/../account.klefki", 404}, {"/manifest.webmanifest", 200}, {"/font.woff2", 200}, {"/pdf.worker.mjs", 200}} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", tt.path, nil))
		if w.Code != tt.status {
			t.Fatalf("%s: %d", tt.path, w.Code)
		}
		if tt.status == 200 {
			if !strings.Contains(w.Header().Get("Content-Security-Policy"), "connect-src 'self'") {
				t.Fatal("missing CSP")
			}
			if !strings.Contains(w.Header().Get("Content-Security-Policy"), "font-src 'self'") {
				t.Fatal("missing local font policy")
			}
			if w.Header().Get("Cache-Control") != "no-cache" {
				t.Fatal("shell must revalidate")
			}
		}
	}
	if _, e := WebHandler(api, t.TempDir()); e == nil {
		t.Fatal("missing build accepted")
	}
}

func TestModelAssetsStayScoped(t *testing.T) {
	dir := t.TempDir()
	if e := os.MkdirAll(filepath.Join(dir, "models", "runtime"), 0700); e != nil {
		t.Fatal(e)
	}
	for name, body := range map[string]string{"index.html": "public shell", "private.json": "must not serve", "models/model.onnx": "public model", "models/config.json": "{}", "models/runtime/test.wasm": "wasm", "models/runtime/test.mjs": "// runtime"} {
		if e := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); e != nil {
			t.Fatal(e)
		}
	}
	h, e := WebHandler(http.NotFoundHandler(), dir)
	if e != nil {
		t.Fatal(e)
	}
	for _, tt := range []struct {
		path   string
		status int
		mime   string
	}{{"/private.json", 404, ""}, {"/models/model.onnx", 200, "application/octet-stream"}, {"/models/config.json", 200, ""}, {"/models/runtime/test.wasm", 200, "application/wasm"}, {"/models/runtime/test.mjs", 200, "text/javascript"}} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", tt.path, nil))
		if w.Code != tt.status {
			t.Fatal(tt.path, w.Code)
		}
		if tt.mime != "" && w.Header().Get("Content-Type") != tt.mime {
			t.Fatal("asset MIME", tt.path)
		}
	}
}
