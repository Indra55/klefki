package semantic

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

//go:embed manifest.json
var manifestJSON []byte

type Asset struct {
	Path     string `json:"path"`
	URL      string `json:"url"`
	Member   string `json:"member,omitempty"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
	Platform string `json:"platform"`
}
type Manifest struct {
	Version          int     `json:"version"`
	Model            string  `json:"model"`
	Revision         string  `json:"revision"`
	Dimension        int     `json:"dimension"`
	Window           int     `json:"window"`
	Overlap          int     `json:"overlap"`
	MaxTokens        int     `json:"maxTokens"`
	Pooling          string  `json:"pooling"`
	QueryInstruction string  `json:"queryInstruction"`
	SearchCutoff     float64 `json:"searchCutoff"`
	SearchRelative   float64 `json:"searchRelative"`
	Runtime          string  `json:"runtime"`
	Assets           []Asset `json:"assets"`
}

func ModelManifest() Manifest { var m Manifest; _ = json.Unmarshal(manifestJSON, &m); return m }
func Fingerprint() string {
	m := ModelManifest()
	s := fmt.Sprintf("%s:%d:%d:%s:%s:labels-v1", m.Revision, m.Window, m.Overlap, m.Pooling, m.QueryInstruction)
	for _, a := range m.Assets {
		if a.Path == "onnx/model.onnx" || a.Path == "tokenizer.json" {
			s += ":" + a.SHA256
		}
	}
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
func assets() []Asset {
	out := []Asset{}
	for _, a := range ModelManifest().Assets {
		if a.Platform == "all" || a.Platform == runtime.GOOS+"-"+runtime.GOARCH {
			out = append(out, a)
		}
	}
	return out
}
func DownloadSize() int64 {
	var n int64
	for _, a := range assets() {
		n += a.Size
	}
	return n
}

func checkedFile(path string, a Asset) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() != a.Size {
		return ErrModel
	}
	f, err := os.Open(path)
	if err != nil {
		return ErrModel
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil || hex.EncodeToString(h.Sum(nil)) != a.SHA256 {
		return ErrModel
	}
	return nil
}
func Verify(root string) error {
	if len(assets()) < 6 {
		return ErrUnavailable
	}
	for _, a := range assets() {
		if err := checkedFile(filepath.Join(root, filepath.FromSlash(a.Path)), a); err != nil {
			return err
		}
	}
	return nil
}

// Install accepts a local bundle directory, or downloads public assets when source is empty.
func Install(ctx context.Context, root, source string, progress func(int64, int64)) error {
	if !Supported() {
		return ErrUnavailable
	}
	if Verify(root) == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(root), 0700); err != nil {
		return ErrModel
	}
	stage, err := os.MkdirTemp(filepath.Dir(root), ".model-*")
	if err != nil {
		return ErrModel
	}
	defer os.RemoveAll(stage)
	client := &http.Client{Timeout: 10 * time.Minute}
	archives := map[string]string{}
	done := int64(0)
	total := DownloadSize()
	for _, a := range assets() {
		if err := ctx.Err(); err != nil {
			return err
		}
		path := filepath.Join(stage, filepath.FromSlash(a.Path))
		if os.MkdirAll(filepath.Dir(path), 0700) != nil {
			return ErrModel
		}
		if source != "" {
			src := filepath.Join(source, filepath.FromSlash(a.Path))
			if checkedFile(src, a) != nil {
				return ErrModel
			}
			f, err := os.Open(src)
			if err != nil {
				return ErrModel
			}
			err = copyAsset(ctx, path, f, a)
			f.Close()
			if err != nil {
				return err
			}
		} else if a.Member == "" {
			if err := download(ctx, client, a.URL, path, a.Size, func(n int64) {
				if progress != nil {
					progress(done+n, total)
				}
			}); err != nil {
				return err
			}
		} else {
			archive := archives[a.URL]
			if archive == "" {
				archive = filepath.Join(stage, "archive-"+a.Platform)
				if err := download(ctx, client, a.URL, archive, 200<<20, nil); err != nil {
					return err
				}
				archives[a.URL] = archive
			}
			if err := extract(ctx, archive, path, a); err != nil {
				return err
			}
		}
		if checkedFile(path, a) != nil {
			return ErrModel
		}
		done += a.Size
		if progress != nil {
			progress(done, total)
		}
	}
	for _, p := range archives {
		os.Remove(p)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// A failed installation cannot replace a valid bundle. Invalid prior bundles are retained.
	if _, err := os.Stat(root); err == nil {
		old := root + ".invalid-" + filepath.Base(stage)
		if os.Rename(root, old) != nil {
			return ErrModel
		}
		if os.Rename(stage, root) != nil {
			os.Rename(old, root)
			return ErrModel
		}
		return nil
	}
	if os.Rename(stage, root) != nil {
		return ErrModel
	}
	return nil
}

type contextReader struct {
	ctx      context.Context
	r        io.Reader
	done     int64
	progress func(int64)
	last     time.Time
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, e := r.r.Read(p)
	r.done += int64(n)
	if r.progress != nil && time.Since(r.last) > 100*time.Millisecond {
		r.last = time.Now()
		r.progress(r.done)
	}
	return n, e
}
func copyAsset(ctx context.Context, path string, r io.Reader, a Asset) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return ErrModel
	}
	n, e := io.Copy(f, io.LimitReader(&contextReader{ctx: ctx, r: r}, a.Size+1))
	syncErr := f.Sync()
	closeErr := f.Close()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if e != nil || n != a.Size || syncErr != nil || closeErr != nil {
		return ErrModel
	}
	return nil
}
func download(ctx context.Context, c *http.Client, url, path string, limit int64, progress func(int64)) error {
	if !strings.HasPrefix(url, "https://") {
		return ErrModel
	}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return ErrModel
	}
	resp, err := c.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || resp.Request.URL.Scheme != "https" {
		return ErrUnavailable
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return ErrModel
	}
	n, e := io.Copy(f, io.LimitReader(&contextReader{ctx: ctx, r: resp.Body, progress: progress}, limit+1))
	s := f.Sync()
	x := f.Close()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if e != nil || n > limit || s != nil || x != nil {
		return ErrModel
	}
	return nil
}
func extract(ctx context.Context, archive, path string, a Asset) error {
	if strings.HasSuffix(a.URL, ".zip") {
		z, err := zip.OpenReader(archive)
		if err != nil {
			return ErrModel
		}
		defer z.Close()
		for _, f := range z.File {
			if filepath.Base(f.Name) == a.Member && strings.Contains(f.Name, "/lib/") {
				r, e := f.Open()
				if e != nil {
					return ErrModel
				}
				defer r.Close()
				return copyAsset(ctx, path, r, a)
			}
		}
	} else {
		f, err := os.Open(archive)
		if err != nil {
			return ErrModel
		}
		defer f.Close()
		gz, err := gzip.NewReader(f)
		if err != nil {
			return ErrModel
		}
		defer gz.Close()
		t := tar.NewReader(io.LimitReader(&contextReader{ctx: ctx, r: gz}, 200<<20))
		for {
			h, e := t.Next()
			if e == io.EOF {
				break
			}
			if e != nil {
				return ErrModel
			}
			if h.Typeflag == tar.TypeReg && filepath.Base(h.Name) == a.Member && strings.Contains(h.Name, "/lib/") {
				return copyAsset(ctx, path, t, a)
			}
		}
	}
	return ErrModel
}
