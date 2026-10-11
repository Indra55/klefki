package server

import (
	"errors"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// WebHandler serves the built PWA on the API origin. Only public build assets belong in dir.
func WebHandler(api http.Handler, dir string) (http.Handler, error) {
	root, e := filepath.Abs(dir)
	if e != nil {
		return nil, e
	}
	if info, e := os.Stat(filepath.Join(root, "index.html")); e != nil || !info.Mode().IsRegular() {
		return nil, errors.New("web build missing")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/v1" || strings.HasPrefix(r.URL.Path, "/v1/") {
			api.ServeHTTP(w, r)
			return
		}
		name := r.URL.Path
		if name == "/" {
			name = "/index.html"
		}
		if path.Clean(name) != name || strings.Contains(name, "\\") || strings.HasPrefix(name, "/.") {
			http.NotFound(w, r)
			return
		}
		modelAsset := strings.HasPrefix(name, "/models/")
		switch path.Ext(name) {
		case ".html", ".js", ".mjs", ".css", ".svg", ".webmanifest", ".png", ".woff", ".woff2":
		case ".onnx", ".wasm", ".json":
			if !modelAsset {
				http.NotFound(w, r)
				return
			}
		default:
			http.NotFound(w, r)
			return
		}
		file := filepath.Join(root, filepath.FromSlash(name))
		info, e := os.Lstat(file)
		if e != nil || !info.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self' 'wasm-unsafe-eval'; style-src 'self'; font-src 'self'; img-src 'self' blob:; connect-src 'self'; worker-src 'self'; manifest-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
		if strings.HasSuffix(name, ".wasm") {
			w.Header().Set("Content-Type", "application/wasm")
		}
		if strings.HasSuffix(name, ".onnx") {
			w.Header().Set("Content-Type", "application/octet-stream")
		}
		if strings.HasSuffix(name, ".mjs") {
			w.Header().Set("Content-Type", "text/javascript")
		}
		if strings.HasSuffix(name, ".webmanifest") {
			w.Header().Set("Content-Type", "application/manifest+json")
		}
		http.ServeFile(w, r, file)
	}), nil
}
