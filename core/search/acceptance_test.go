package search_test

import (
	"bytes"
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	vc "klefki/core/crypto"
	"klefki/core/search"
	vs "klefki/core/sync"
	"klefki/server"
)

func TestOCRSearchEncryptedSyncAcceptance(t *testing.T) {
	for _, tool := range []string{"tesseract", "pdfinfo", "pdfimages", "pdftotext", "pdftoppm"} {
		if _, e := exec.LookPath(tool); e != nil {
			if os.Getenv("KLEFKI_REQUIRE_OCR") == "1" {
				t.Fatal("required OCR tools unavailable")
			}
			t.Skip("local OCR tools unavailable; set KLEFKI_REQUIRE_OCR=1 to require acceptance")
		}
	}
	root := t.TempDir()
	a, key, _, e := vc.NewAccount([]byte("public M7 test password"))
	if e != nil {
		t.Fatal(e)
	}
	defer vc.Wipe(key[:])
	keys, e := vc.Derive([]byte("public M7 test password"), a.Descriptor)
	if e != nil {
		t.Fatal(e)
	}
	defer keys.Clear()
	credential := vc.Credential(keys.Auth, a.ID)
	defer vc.Wipe(credential[:])
	backend, e := server.Open(server.Config{DataDir: filepath.Join(root, "server"), RateBurst: 10000})
	if e != nil {
		t.Fatal(e)
	}
	defer backend.Close()
	web := httptest.NewTLSServer(backend)
	defer web.Close()
	remote, e := vs.NewRemote(web.URL, web.Client(), false)
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	session, e := remote.Signup(ctx, a, credential)
	if e != nil {
		t.Fatal(e)
	}
	s, e := vs.Open(filepath.Join(root, "a.db"), a.ID)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	for _, name := range []string{"scan.png", "scan.pdf", "mixed.pdf"} {
		data, e := os.ReadFile(filepath.Join("testdata", name))
		if e != nil {
			t.Fatal(e)
		}
		id, _ := vc.RandomID()
		if _, e = s.Put(key, id.String(), "", vc.Metadata{Type: "file", Name: name, Tags: []string{}, Size: uint64(len(data))}, bytes.NewReader(data)); e != nil {
			t.Fatal(e)
		}
	}
	report, e := search.Build(ctx, s, key, search.Extractor{TempDir: root})
	if e != nil || report.Documents != 3 {
		t.Fatal("real OCR build", e)
	}
	matches, e := search.Query(ctx, s, key, "nebula")
	if e != nil || len(matches) != 3 {
		t.Fatal("image/scanned PDF search", e)
	}
	matches, e = search.Query(ctx, s, key, "orbital")
	if e != nil || len(matches) != 1 || matches[0].Name != "scan.pdf" {
		t.Fatal("second scanned page", e)
	}
	matches, e = search.Query(ctx, s, key, "selectable")
	if e != nil || len(matches) != 1 || matches[0].Name != "mixed.pdf" {
		t.Fatal("selectable text lost on mixed page", e)
	}
	entries, e := os.ReadDir(root)
	if e != nil {
		t.Fatal(e)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "klefki-ocr-") {
			t.Fatal("plaintext staging retained")
		}
	}
	c := vs.Client{Store: s, Remote: remote, Session: session, Master: key}
	defer vc.Wipe(c.Master[:])
	if e = c.Sync(ctx); e != nil {
		t.Fatal(e)
	}
	other, e := vs.Open(filepath.Join(root, "b.db"), a.ID)
	if e != nil {
		t.Fatal(e)
	}
	defer other.Close()
	login, e := remote.Login(ctx, a.ID, credential)
	if e != nil {
		t.Fatal(e)
	}
	c2 := vs.Client{Store: other, Remote: remote, Session: login, Master: key}
	defer vc.Wipe(c2.Master[:])
	if e = c2.Sync(ctx); e != nil {
		t.Fatal(e)
	}
	// Searching a synced index never invokes extraction or needs an OCR installation.
	t.Setenv("PATH", "")
	matches, e = search.Query(ctx, other, key, "7319")
	if e != nil || len(matches) != 3 {
		t.Fatal("synced index search", e)
	}
	snapshot := filepath.Join(root, "restored.db")
	if e = other.Snapshot(snapshot); e != nil {
		t.Fatal(e)
	}
	restored, e := vs.OpenExisting(snapshot, a.ID)
	if e != nil {
		t.Fatal(e)
	}
	defer restored.Close()
	if matches, e = search.Query(ctx, restored, key, "8642"); e != nil || len(matches) != 1 {
		t.Fatal("backup index search", e)
	}
	// Check server SQLite/WAL and blob storage, plus both local encrypted stores.
	e = filepath.WalkDir(root, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		data, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		for _, secret := range []string{"nebula", "NEBULA", "orbital", "ORBITAL", "certificate", "search-index.v1", "scan.pdf"} {
			if bytes.Contains(data, []byte(secret)) {
				t.Fatal("plaintext index or metadata leaked to persisted storage")
			}
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
}
