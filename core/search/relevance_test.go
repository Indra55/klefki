package search

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	vc "klefki/core/crypto"
	"klefki/core/semantic"
	vs "klefki/core/sync"
)

const balanceQuestion = "Which record combines a consumption history visualization with instructions for settling an outstanding balance?"

func TestLongQueryRelevance(t *testing.T) {
	account, _ := vc.RandomID()
	key, _ := vc.RandomKey()
	defer vc.Wipe(key[:])
	s, err := vs.Open(filepath.Join(t.TempDir(), "local.db"), account)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	add := func(name, body string) string {
		t.Helper()
		id, _ := vc.RandomID()
		revision, err := s.Put(key, id.String(), "", vc.Metadata{Type: "file", Name: name, Tags: []string{}, Size: uint64(len(body))}, bytes.NewBufferString(body))
		if err != nil {
			t.Fatal(err)
		}
		return revision
	}
	bill := add("utility.txt", "Electricity statement: consumption history visualization, outstanding balance INR 1500. Payment instructions: settle your bill online. Account 731924.")
	resume := add("resume.txt", "A software engineer with employment history and experience developing applications for clients.")
	add("match.txt", "A football match with a score of 2 to 1. Highlights for fans.")
	add("unrelated.txt", "Instructions for making a chart with a visualization of tournament results.")
	ctx := context.Background()
	if _, err := Build(ctx, s, key, Extractor{}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		query string
		want  string
	}{
		{balanceQuestion, bill},
		{"Which document contains the identifier 731924?", bill},
		{"employment", resume},
		{"a with for an", ""},
		{"Which record describes the orbital mechanics of a lunar spacecraft?", ""},
	} {
		hits, err := Query(ctx, s, key, tc.query)
		if err != nil {
			t.Fatal(err)
		}
		if tc.want == "" {
			if len(hits) != 0 {
				t.Errorf("%q: expected no matches, got %v", tc.query, hits)
			}
		} else if len(hits) != 1 || hits[0].Revision != tc.want {
			t.Errorf("%q: expected only %s, got %v", tc.query, tc.want, hits)
		}
	}
}

func TestNativeLongHybridQueryRelevance(t *testing.T) {
	root := os.Getenv("KLEFKI_MODEL_DIR")
	if root == "" || !semantic.Supported() {
		if os.Getenv("KLEFKI_REQUIRE_ONNX") == "1" {
			t.Fatal("required ONNX unavailable")
		}
		t.Skip("requires native model")
	}
	model, err := semantic.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer model.Close()
	account, _ := vc.RandomID()
	key, _ := vc.RandomKey()
	defer vc.Wipe(key[:])
	s, err := vs.Open(filepath.Join(t.TempDir(), "local.db"), account)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var bill string
	for i, d := range []struct{ name, text string }{
		{"utility.txt", "Electricity statement: consumption history visualization, outstanding balance INR 1500. Payment instructions: settle your bill online. Account 731924."},
		{"resume.txt", "A software engineer with employment history and experience developing applications for clients."},
		{"match.txt", "A football match with a score of 2 to 1. Highlights for fans."},
		{"unrelated.txt", "Instructions for making a chart with a visualization of tournament results."},
	} {
		id, _ := vc.RandomID()
		revision, err := s.Put(key, id.String(), "", vc.Metadata{Type: "file", Name: d.name, Tags: []string{}, Size: uint64(len(d.text))}, bytes.NewBufferString(d.text))
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			bill = revision
		}
	}
	ctx := context.Background()
	if _, err := BuildSemantic(ctx, s, key, Extractor{}, model); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{balanceQuestion, "Which document contains the identifier 731924?"} {
		hits, err := QueryMode(ctx, s, key, model, query, "hybrid")
		if err != nil || len(hits) != 1 || hits[0].Revision != bill {
			t.Fatalf("%q: expected only utility bill, got %v (%v)", query, hits, err)
		}
	}
	for _, query := range []string{"Which record describes the orbital mechanics of a lunar spacecraft?"} {
		hits, err := QueryMode(ctx, s, key, model, query, "hybrid")
		if err != nil || len(hits) != 0 {
			t.Fatalf("%q: expected no matches, got %v (%v)", query, hits, err)
		}
	}
}
