package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"vault/core/search"
	"vault/core/semantic"
)

func TestM8CLIOptInAndCorrections(t *testing.T) {
	model := os.Getenv("VAULT_MODEL_DIR")
	if model == "" || !semantic.Supported() {
		t.Skip("requires real native model")
	}
	root := t.TempDir()
	account := filepath.Join(root, "account.v1")
	password := filepath.Join(root, "password")
	recovery := filepath.Join(root, "recovery")
	for name, text := range map[string]string{"password": "public M8 CLI fixture password", "bill.txt": "PUBLIC SYNTHETIC electricity utility bill. Issued 2026-04-12. Amount INR 123.45.", "query": "payment for household power"} {
		if e := os.WriteFile(filepath.Join(root, name), []byte(text), 0600); e != nil {
			t.Fatal(e)
		}
	}
	if e := run([]string{"init", "--vault", account, "--password-file", password, "--recovery-out", recovery}, io.Discard); e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	invoke := func(command string, args ...string) error {
		out.Reset()
		return run(append([]string{command, "--vault", account, "--password-file", password}, args...), &out)
	}
	if e := invoke("add", filepath.Join(root, "bill.txt")); e != nil {
		t.Fatal(e)
	}
	if e := invoke("index", "--semantic", "--model-dir", model); e != nil {
		t.Fatal(e)
	}
	if e := invoke("search", "--mode", "hybrid", "--model-dir", model, "--query-file", filepath.Join(root, "query")); e != nil {
		t.Fatal(e)
	}
	var hits []search.Hit
	if json.Unmarshal(out.Bytes(), &hits) != nil || len(hits) != 1 {
		t.Fatal("CLI hybrid retrieval")
	}
	rev := hits[0].Revision
	if e := invoke("analyze", "--revision", rev); e != nil {
		t.Fatal(e)
	}
	var a search.Analysis
	if json.Unmarshal(out.Bytes(), &a) != nil || len(a.Fields) != 2 {
		t.Fatal("CLI fields")
	}
	data, _ := json.Marshal(struct {
		Category string           `json:"category"`
		Fields   []semantic.Field `json:"fields"`
		Resolve  []string         `json:"resolve"`
	}{"Money", a.Fields, []string{}})
	path := filepath.Join(root, "correction.json")
	if e := os.WriteFile(path, data, 0600); e != nil {
		t.Fatal(e)
	}
	if e := invoke("correct", "--revision", rev, "--correction-file", path); e != nil {
		t.Fatal(e)
	}
	if e := invoke("index"); e != nil {
		t.Fatal("keyword-only rebuild", e)
	}
	if e := invoke("analyze", "--revision", rev); e != nil {
		t.Fatal(e)
	}
	if json.Unmarshal(out.Bytes(), &a) != nil || a.Confirmed != "Money" {
		t.Fatal("CLI correction persisted")
	}
}
