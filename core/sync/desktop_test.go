package sync_test

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	vc "vault/core/crypto"
	vs "vault/core/sync"
)

func TestMetadataEditRetainsContentAndSyncs(t *testing.T) {
	h := setup(t)
	a, ca := h.device(t, "edit-a", true)
	b, cb := h.device(t, "edit-b", false)
	id, _ := vc.RandomID()
	original := add(t, a, h.master, id.String(), "", "private-original.txt", "unchanged content")
	before, _ := a.Revision(h.master, original)
	edited, e := a.Edit(h.master, original, vs.Edit{Name: "private-renamed.txt", Tags: []string{"private-tag"}, Favorite: true})
	if e != nil {
		t.Fatal(e)
	}
	after, _ := a.Revision(h.master, edited)
	if before.Metadata.FileID != after.Metadata.FileID {
		t.Fatal("metadata edit replaced content")
	}
	if _, e = a.Edit(h.master, original, vs.Edit{Name: "stale", Tags: []string{}}); !errors.Is(e, vs.ErrConflict) {
		t.Fatal("stale edit accepted", e)
	}
	if e = ca.Sync(context.Background()); e != nil {
		t.Fatal(e)
	}
	if e = cb.Sync(context.Background()); e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	if e = b.Export(h.master, edited, &out); e != nil || out.String() != "unchanged content" {
		t.Fatal("content changed", e)
	}
	heads, _ := b.Heads(h.master)
	if len(heads) != 1 || heads[0].Metadata.Name != "private-renamed.txt" || !heads[0].Metadata.Favorite {
		t.Fatal("metadata lost")
	}
}
func TestStoreLeaseAndCancellation(t *testing.T) {
	id, _ := vc.RandomID()
	path := filepath.Join(t.TempDir(), "local.db")
	s, e := vs.Open(path, id)
	if e != nil {
		t.Fatal(e)
	}
	if other, e := vs.Open(path, id); e == nil {
		other.Close()
		t.Fatal("second writer accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	key, _ := vc.RandomKey()
	obj, _ := vc.RandomID()
	if _, e = s.PutContext(ctx, key, obj.String(), "", vc.Metadata{Type: "file", Name: "canceled", Tags: []string{}, Size: 4}, bytes.NewBufferString("test")); e == nil {
		t.Fatal("canceled import accepted")
	}
	heads, _ := s.Heads(key)
	if len(heads) != 0 {
		t.Fatal("partial import")
	}
	s.Close()
	again, e := vs.Open(path, id)
	if e != nil {
		t.Fatal(e)
	}
	again.Close()
}
