package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"vault/core/search"
)

func TestCLISearchWorkflow(t *testing.T) {
	root := t.TempDir()
	account := filepath.Join(root, "account.v1")
	password := filepath.Join(root, "password")
	recovery := filepath.Join(root, "recovery")
	for name, body := range map[string]string{"password": "public M7 CLI fixture password", "file.txt": "orbital certificate", "query": "orbital"} {
		if e := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); e != nil {
			t.Fatal(e)
		}
	}
	if e := run([]string{"init", "--vault", account, "--password-file", password, "--recovery-out", recovery}, io.Discard); e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	invoke := func(command string, extra ...string) error {
		out.Reset()
		return run(append([]string{command, "--vault", account, "--password-file", password}, extra...), &out)
	}
	if e := invoke("add", filepath.Join(root, "file.txt")); e != nil {
		t.Fatal(e)
	}
	if e := invoke("search", "--query-file", filepath.Join(root, "query")); e == nil {
		t.Fatal("missing index accepted")
	}
	if e := invoke("index"); e != nil {
		t.Fatal(e)
	}
	if e := invoke("search", "--query-file", filepath.Join(root, "query")); e != nil {
		t.Fatal(e)
	}
	var hits []search.Hit
	if json.Unmarshal(out.Bytes(), &hits) != nil || len(hits) != 1 || hits[0].Name != "file.txt" {
		t.Fatal("CLI content search")
	}
	if e := run([]string{"search", "--vault", account, "--recovery-file", recovery, "--query-file", filepath.Join(root, "query")}, io.Discard); e != nil {
		t.Fatal(e)
	}
}
