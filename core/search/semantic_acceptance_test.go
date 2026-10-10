package search_test

import (
	"bytes"
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	vc "klefki/core/crypto"
	"klefki/core/search"
	"klefki/core/semantic"
	vs "klefki/core/sync"
	"klefki/server"
)

type failingModel struct{ semantic.Embedder }

func (m failingModel) EmbedTokens(context.Context, []int64) ([]float32, error) {
	return nil, semantic.ErrModel
}

type changedModel struct{ semantic.Embedder }

func (m changedModel) Fingerprint() string { return "a-different-model" }

func TestSemanticEncryptedSyncRestoreAndFailure(t *testing.T) {
	root := os.Getenv("KLEFKI_MODEL_DIR")
	if root == "" || !semantic.Supported() {
		if os.Getenv("KLEFKI_REQUIRE_ONNX") == "1" {
			t.Fatal("required ONNX unavailable")
		}
		t.Skip("requires native model")
	}
	model, e := semantic.Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer model.Close()
	a, key, _, e := vc.NewAccount([]byte("public M8 acceptance password"))
	if e != nil {
		t.Fatal(e)
	}
	defer vc.Wipe(key[:])
	keys, e := vc.Derive([]byte("public M8 acceptance password"), a.Descriptor)
	if e != nil {
		t.Fatal(e)
	}
	defer keys.Clear()
	cred := vc.Credential(keys.Auth, a.ID)
	defer vc.Wipe(cred[:])
	dir := t.TempDir()
	backend, e := server.Open(server.Config{DataDir: filepath.Join(dir, "server"), RateBurst: 10000})
	if e != nil {
		t.Fatal(e)
	}
	defer backend.Close()
	httpServer := httptest.NewTLSServer(backend)
	defer httpServer.Close()
	remote, e := vs.NewRemote(httpServer.URL, httpServer.Client(), false)
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	session, e := remote.Signup(ctx, a, cred)
	if e != nil {
		t.Fatal(e)
	}
	s, e := vs.Open(filepath.Join(dir, "a.db"), a.ID)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	id, _ := vc.RandomID()
	text := "PUBLIC SYNTHETIC document. The electric utility billed for household power consumption. Issued 2026-04-12. Amount INR 98765.43."
	rev, e := s.Put(key, id.String(), "", vc.Metadata{Type: "file", Name: "m8-electricity.txt", Tags: []string{}, Size: uint64(len(text))}, bytes.NewBufferString(text))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = search.BuildSemantic(ctx, s, key, search.Extractor{}, model); e != nil {
		t.Fatal(e)
	}
	analysis, e := search.Analyze(ctx, s, key, rev, semantic.DefaultThresholds)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = search.SaveCorrection(ctx, s, key, rev, "Money", analysis.Fields, nil); e != nil {
		t.Fatal(e)
	}
	if _, e = search.BuildSemantic(ctx, s, key, search.Extractor{}, failingModel{model}); !errors.Is(e, semantic.ErrModel) {
		t.Fatal("model failure", e)
	}
	if _, e = search.QueryMode(ctx, s, key, changedModel{model}, "power", "hybrid"); !errors.Is(e, search.ErrStale) {
		t.Fatal("model change", e)
	}
	client := vs.Client{Store: s, Remote: remote, Session: session, Master: key}
	defer vc.Wipe(client.Master[:])
	if e = client.Sync(ctx); e != nil {
		t.Fatal(e)
	}
	b, e := vs.Open(filepath.Join(dir, "b.db"), a.ID)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	client.Store = b
	if e = client.Sync(ctx); e != nil {
		t.Fatal(e)
	}
	assertSearch := func(store *vs.Store) {
		t.Helper()
		h, e := search.QueryMode(ctx, store, key, model, "charges for using electricity", "hybrid")
		if e != nil || len(h) != 1 || h[0].Revision != rev {
			t.Fatal("synced/restored semantic search", e)
		}
		a, e := search.Analyze(ctx, store, key, rev, semantic.DefaultThresholds)
		if e != nil || a.Confirmed != "Money" || len(a.Fields) != 2 {
			t.Fatal("correction restore", e)
		}
	}
	assertSearch(b)
	snapshot := filepath.Join(dir, "restore.db")
	if e = b.Snapshot(snapshot); e != nil {
		t.Fatal(e)
	}
	restored, e := vs.OpenExisting(snapshot, a.ID)
	if e != nil {
		t.Fatal(e)
	}
	assertSearch(restored)
	restored.Close()
	if e = filepath.WalkDir(dir, func(path string, entry os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if entry.IsDir() {
			return nil
		}
		data, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		for _, secret := range []string{"m8-electricity.txt", "household power consumption", "98765.43", "semantic-index.v1.json", "analysis-correction.v1.json"} {
			if bytes.Contains(data, []byte(secret)) {
				t.Fatal("plaintext persisted")
			}
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}
