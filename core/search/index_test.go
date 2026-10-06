package search

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	vc "vault/core/crypto"
	vs "vault/core/sync"
)

func TestIndexRankingFreshnessAndRebuild(t *testing.T) {
	account, _ := vc.RandomID()
	key, _ := vc.RandomKey()
	defer vc.Wipe(key[:])
	s, e := vs.Open(filepath.Join(t.TempDir(), "local.db"), account)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	add := func(name, body string) string {
		id, _ := vc.RandomID()
		r, e := s.Put(key, id.String(), "", vc.Metadata{Type: "file", Name: name, Tags: []string{}, Size: uint64(len(body))}, bytes.NewBufferString(body))
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	first := add("a.txt", "nebula certificate nebula")
	add("b.txt", "nebula unrelated unrelated unrelated unrelated unrelated")
	add("c.txt", "ordinary paperwork")
	ctx := context.Background()
	if _, e := Query(ctx, s, key, "nebula"); !errors.Is(e, ErrStale) {
		t.Fatal("missing index accepted", e)
	}
	report, e := Build(ctx, s, key, Extractor{})
	if e != nil || report.Documents != 3 {
		t.Fatal("build", e)
	}
	hits, e := Query(ctx, s, key, "NEBULA")
	if e != nil || len(hits) != 2 || hits[0].Revision != first || hits[0].Score <= hits[1].Score {
		t.Fatal("ranking", e)
	}
	repeated, _ := Query(ctx, s, key, "nebula nebula")
	if repeated[0].Score != hits[0].Score {
		t.Fatal("duplicate query weighting")
	}
	heads, e := s.Heads(key)
	if e != nil {
		t.Fatal(e)
	}
	for _, h := range heads {
		if IsIndex(h.Metadata) {
			if _, e = s.Edit(key, h.Revision, vs.Edit{Name: h.Metadata.Name, Tags: []string{IndexTag}}); e != nil {
				t.Fatal(e)
			}
		}
	}
	if _, e = Query(ctx, s, key, "nebula"); !errors.Is(e, ErrStale) {
		t.Fatal("old extraction index accepted", e)
	}
	if _, e = Build(ctx, s, key, Extractor{}); e != nil {
		t.Fatal("old index upgrade", e)
	}
	if _, e = s.Edit(key, first, vs.Edit{Name: "changed.txt", Tags: []string{}}); e != nil {
		t.Fatal(e)
	}
	if _, e = Query(ctx, s, key, "nebula"); !errors.Is(e, ErrStale) {
		t.Fatal("stale index used", e)
	}
	if _, e = Build(ctx, s, key, Extractor{}); e != nil {
		t.Fatal(e)
	}
	if hits, e = Query(ctx, s, key, "certificate"); e != nil || len(hits) != 1 || hits[0].Name != "changed.txt" {
		t.Fatal("rebuild", e)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, e = Build(canceled, s, key, Extractor{}); !errors.Is(e, context.Canceled) {
		t.Fatal("cancellation", e)
	}
}

func TestTokenizationAndTrash(t *testing.T) {
	terms := frequencies("CAFÉ café 7319 résumé")
	if terms["café"] != 2 || terms["7319"] != 1 {
		t.Fatal("Unicode tokenization")
	}
	parent := "11111111111111111111111111111111"
	h := []vs.Head{{Object: parent, Metadata: vc.Metadata{Type: "folder", Tags: []string{"vault:system:trash"}}}, {Object: "child", Metadata: vc.Metadata{Type: "file", Parent: &parent, Tags: []string{}}}, {Object: "index", Metadata: vc.Metadata{Type: "file", Tags: []string{IndexTag}}}}
	if len(documents(h)) != 0 {
		t.Fatal("trash or index indexed")
	}
}
