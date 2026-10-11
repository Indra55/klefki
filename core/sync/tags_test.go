package sync_test

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	vc "klefki/core/crypto"
	vs "klefki/core/sync"
)

func TestTagFilesAtomicAndMetadataOnly(t *testing.T) {
	account, _ := vc.RandomID()
	key, _ := vc.RandomKey()
	defer vc.Wipe(key[:])
	store, e := vs.Open(filepath.Join(t.TempDir(), "local.db"), account)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	add := func(name string) string {
		id, _ := vc.RandomID()
		rev, e := store.Put(key, id.String(), "", vc.Metadata{Type: "file", Name: name, Tags: []string{"Personal"}, Favorite: true, Size: 12}, strings.NewReader("private-body"))
		if e != nil {
			t.Fatal(e)
		}
		return rev
	}
	first, second := add("first.txt"), add("second.txt")
	original, _ := store.Revision(key, first)
	newSecond, e := store.Edit(key, second, vs.Edit{Name: "renamed.txt", Tags: []string{"Personal"}})
	if e != nil {
		t.Fatal(e)
	}
	pending, _ := store.Pending()
	ctx := context.Background()
	if n, e := store.TagFiles(ctx, key, []vs.TagChange{{Revision: first, Tag: "Research"}, {Revision: second, Tag: "Research"}}); n != 0 || !errors.Is(e, vs.ErrConflict) {
		t.Fatal("stale batch", n, e)
	}
	if after, _ := store.Pending(); after != pending {
		t.Fatal("partially applied stale batch")
	}
	for _, tag := range []string{"klefki:system:hidden", "KLEFKI:system:hidden", " ", "bad\nname", strings.Repeat("a", 81)} {
		if n, e := store.TagFiles(ctx, key, []vs.TagChange{{Revision: first, Tag: "Research"}, {Revision: newSecond, Tag: tag}}); n != 0 || e == nil {
			t.Fatal("invalid batch", n, e)
		}
	}
	if after, _ := store.Pending(); after != pending {
		t.Fatal("partially applied invalid batch")
	}
	if _, e := store.TagFiles(ctx, key, []vs.TagChange{{Revision: first, Tag: "Research"}, {Revision: first, Tag: "Travel"}}); e == nil {
		t.Fatal("duplicate revision accepted")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, e := store.TagFiles(cancelled, key, []vs.TagChange{{Revision: first, Tag: "Research"}}); e == nil {
		t.Fatal("cancelled batch applied")
	}
	if n, e := store.TagFiles(ctx, key, []vs.TagChange{{Revision: first, Tag: "Research"}, {Revision: newSecond, Tag: "Research"}}); n != 2 || e != nil {
		t.Fatal(n, e)
	}
	heads, e := store.Heads(key)
	if e != nil {
		t.Fatal(e)
	}
	for _, h := range heads {
		if h.Metadata.Tags[0] != "Personal" || h.Metadata.Tags[1] != "Research" {
			t.Fatal("existing tags lost", h.Metadata)
		}
		if h.Object == original.Object && (h.Metadata.FileID != original.Metadata.FileID || !h.Metadata.Favorite || h.Metadata.Name != original.Metadata.Name) {
			t.Fatal("changed unrelated metadata or content binding")
		}
		var b bytes.Buffer
		if e := store.Export(key, h.Revision, &b); e != nil || b.String() != "private-body" {
			t.Fatal("content changed", e)
		}
		if n, e := store.TagFiles(ctx, key, []vs.TagChange{{Revision: h.Revision, Tag: "research"}}); n != 0 || e != nil {
			t.Fatal("case duplicate added", n, e)
		}
	}
}

func TestTagFilesEncryptedSyncAndRestart(t *testing.T) {
	h := setup(t)
	a, ca := h.device(t, "tag-a", true)
	b, cb := h.device(t, "tag-b", false)
	id, _ := vc.RandomID()
	original := add(t, a, h.master, id.String(), "", "private-tagged.txt", "unchanged tagged content")
	before, e := a.Revision(h.master, original)
	if e != nil {
		t.Fatal(e)
	}
	syncNow(t, ca)
	syncNow(t, cb)
	const tag = "Private astronomy collection"
	if n, e := a.TagFiles(context.Background(), h.master, []vs.TagChange{{Revision: original, Tag: tag}}); e != nil || n != 1 {
		t.Fatal(n, e)
	}
	// The offline queue and reviewed tags survive closing the store.
	a.Close()
	a, e = vs.Open(filepath.Join(h.root, "tag-a", "local.db"), h.account.ID)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	ca.Store = a
	syncNow(t, ca)
	syncNow(t, cb)
	heads, e := b.Heads(h.master)
	if e != nil || len(heads) != 1 {
		t.Fatal(heads, e)
	}
	m := heads[0].Metadata
	if len(m.Tags) != 2 || m.Tags[1] != tag || m.FileID != before.Metadata.FileID {
		t.Fatal("tag or content binding lost", m)
	}
	if content(t, b, h.master, heads[0].Revision) != "unchanged tagged content" || content(t, b, h.master, original) != "unchanged tagged content" {
		t.Fatal("content or history changed")
	}
	scan(t, h.root, [][]byte{[]byte(tag), []byte("private-tagged.txt"), []byte("unchanged tagged content")})
}
