package desktop

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	vc "klefki/core/crypto"
	"klefki/core/semantic"
	vs "klefki/core/sync"
)

func TestOrganizationApplyRejectsStaleHiddenAndWrongGeneration(t *testing.T) {
	s, _ := testService(t)
	rev := put(t, s, "document.txt", nil)
	st := s.State()
	if _, e := s.SuggestOrganization(nil); e == nil || e.Error() != "SEMANTIC_DISABLED" {
		t.Fatal("organizer without opt in", e)
	}
	if _, e := s.ApplyOrganization(st.Generation+1, []vs.TagChange{{Revision: rev, Tag: "Research"}}); !errors.Is(e, vs.ErrConflict) {
		t.Fatal("wrong session generation accepted", e)
	}
	if n, e := s.ApplyOrganization(st.Generation, []vs.TagChange{{Revision: rev, Tag: "Research"}}); e != nil || n != 1 {
		t.Fatal(n, e)
	}
	list, _ := s.List()
	item := list.Items[0]
	if len(item.Tags) != 2 || item.Tags[0] != "private-desktop-tag" || item.Tags[1] != "Research" {
		t.Fatal("existing tags lost", item)
	}
	if _, e := s.ApplyOrganization(st.Generation, []vs.TagChange{{Revision: rev, Tag: "Other"}}); !errors.Is(e, vs.ErrConflict) {
		t.Fatal("stale source accepted", e)
	}
	trash, e := s.Trash(item.Revision)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := s.ApplyOrganization(st.Generation, []vs.TagChange{{Revision: trash, Tag: "Other"}}); e == nil {
		t.Fatal("trashed source accepted")
	}
	folder, e := s.Folder("Hidden folder", nil)
	if e != nil {
		t.Fatal(e)
	}
	f := find(t, s, folder)
	child := put(t, s, "nested.txt", &f.Object)
	if _, e := s.Trash(folder); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ApplyOrganization(st.Generation, []vs.TagChange{{Revision: child, Tag: "Other"}}); e == nil {
		t.Fatal("trash descendant accepted")
	}
	s.Lock()
	if _, e := s.ApplyOrganization(st.Generation, []vs.TagChange{{Revision: item.Revision, Tag: "Other"}}); !errors.Is(e, ErrLocked) {
		t.Fatal("apply after lock", e)
	}
}

func TestNativeOrganizationPreviewAndApply(t *testing.T) {
	root := os.Getenv("KLEFKI_MODEL_DIR")
	if root == "" || !semantic.Supported() {
		if os.Getenv("KLEFKI_REQUIRE_ONNX") == "1" {
			t.Fatal("required ONNX unavailable")
		}
		t.Skip("requires native model")
	}
	s, _ := testService(t)
	bodies := map[string]string{
		"invoice.txt":        "Invoice for electricity supply. Total amount payable INR 1,240.00. Payment due on 2026-10-31. Electricity consumption and monthly bill charges.",
		"vegetable soup.txt": "Recipe for vegetable soup. Ingredients: carrots, potatoes, onions and vegetable broth. Chop the vegetables, simmer in a pot for twenty minutes and serve the soup hot.",
		"tomato soup.txt":    "Recipe for tomato soup. Ingredients: tomatoes, onion, vegetable broth and basil. Chop the vegetables, simmer in a pot for twenty minutes and serve the soup hot.",
	}
	if e := s.with(func(x *session) error {
		for name, body := range bodies {
			id, _ := vc.RandomID()
			if _, e := x.store.Put(x.master, id.String(), "", vc.Metadata{Type: "file", Name: name, Tags: []string{}, Size: uint64(len(body))}, strings.NewReader(body)); e != nil {
				return e
			}
		}
		if e := semantic.Install(x.ctx, s.modelRoot(), root, nil); e != nil {
			return e
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if e := s.ConfigureSemantic(SemanticPreferences{Enabled: true, Thresholds: semantic.DefaultThresholds}); e != nil {
		t.Fatal(e)
	}
	plan, e := s.SuggestOrganization([]string{"Recipes"})
	if e != nil || len(plan.Suggestions) != 3 {
		t.Fatal("native preview", plan, e)
	}
	changes := []vs.TagChange{}
	for _, row := range plan.Suggestions {
		want := "Recipes"
		if row.Name == "invoice.txt" {
			want = "Money"
		}
		if row.Tag != want || row.New {
			t.Errorf("%s: got %q, want %q (basis %s, similarity %.3f)", row.Name, row.Tag, want, row.Basis, row.Score)
		}
		changes = append(changes, vs.TagChange{Revision: row.Revision, Tag: want})
	}
	if e := s.ConfigureSemantic(SemanticPreferences{Enabled: true, Thresholds: semantic.Thresholds{Cutoff: .99, Margin: .01}}); e != nil {
		t.Fatal(e)
	}
	grouped, e := s.SuggestOrganization(nil)
	if e != nil || len(grouped.Suggestions) != 3 {
		t.Fatal("unmatched native groups", grouped, e)
	}
	tags := map[string]string{}
	for _, row := range grouped.Suggestions {
		if !row.New {
			t.Fatal("unmatched document forced into existing tag", row)
		}
		tags[row.Name] = row.Tag
	}
	if tags["vegetable soup.txt"] != tags["tomato soup.txt"] || tags["invoice.txt"] == tags["tomato soup.txt"] {
		t.Fatal("native embeddings did not separate content groups", tags)
	}
	if e := s.ConfigureSemantic(SemanticPreferences{Enabled: true, Thresholds: semantic.DefaultThresholds}); e != nil {
		t.Fatal(e)
	}
	list, _ := s.List()
	for _, item := range list.Items {
		if len(item.Tags) != 0 {
			t.Fatal("preview applied tags")
		}
	}
	if n, e := s.ApplyOrganization(plan.Generation, changes); e != nil || n != 3 {
		t.Fatal("native apply", n, e)
	}
	list, e = s.List()
	if e != nil || len(list.Items) != 3 {
		t.Fatal(list, e)
	}
	for _, item := range list.Items {
		if len(item.Tags) != 1 {
			t.Fatal("missing saved tag", item)
		}
		if e := s.with(func(x *session) error {
			var b bytes.Buffer
			if e := x.store.ExportContext(context.Background(), x.master, item.Revision, &b); e != nil {
				return e
			}
			if b.String() != bodies[item.Name] {
				t.Fatal("organization changed file contents")
			}
			return nil
		}); e != nil {
			t.Fatal(e)
		}
	}
}
