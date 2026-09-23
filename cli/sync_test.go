package main

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"vault/server"
)

func TestCLIOfflineSyncWorkflow(t *testing.T) {
	dir := t.TempDir()
	account := filepath.Join(dir, "account.v1")
	password := filepath.Join(dir, "password")
	source := filepath.Join(dir, "private-synthetic-name.txt")
	body := []byte("synthetic offline CLI content")
	if os.WriteFile(password, []byte("public CLI sync password"), 0600) != nil || os.WriteFile(source, body, 0600) != nil {
		t.Fatal("fixture setup")
	}
	var out bytes.Buffer
	if e := run([]string{"init", "--vault", account, "--password-file", password, "--recovery-out", filepath.Join(dir, "recovery")}, &out); e != nil {
		t.Fatal(e)
	}
	common := []string{"--vault", account, "--password-file", password}
	invoke := func(command string, extra ...string) error {
		out.Reset()
		args := append([]string{command}, common...)
		return run(append(args, extra...), &out)
	}
	a := filepath.Join(dir, "a.db")
	b := filepath.Join(dir, "b.db")
	if e := invoke("add", "--store", a, source); e != nil {
		t.Fatal(e)
	}
	var added struct {
		Object   string `json:"object"`
		Revision string `json:"revision"`
	}
	if json.Unmarshal(out.Bytes(), &added) != nil || added.Revision == "" {
		t.Fatal("offline add receipt")
	}
	s, e := server.Open(server.Config{DataDir: filepath.Join(dir, "server"), AllowHTTP: true, RateBurst: 1000})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	web := httptest.NewServer(s)
	defer web.Close()
	if e = invoke("register", "--server", web.URL, "--dev-http"); e != nil {
		t.Fatal(e)
	}
	for _, store := range []string{a, b} {
		if e = invoke("sync", "--store", store, "--server", web.URL, "--dev-http"); e != nil {
			t.Fatal(e)
		}
	}
	if e = invoke("list", "--store", b); e != nil {
		t.Fatal(e)
	}
	if !bytes.Contains(out.Bytes(), []byte(added.Revision)) || bytes.Contains(out.Bytes(), []byte("private-synthetic-name")) {
		t.Fatal("list IDs or privacy")
	}
	dest := filepath.Join(dir, "exported")
	if e = invoke("export", "--store", b, "-o", dest, added.Revision); e != nil {
		t.Fatal(e)
	}
	got, e := os.ReadFile(dest)
	if e != nil || !bytes.Equal(got, body) {
		t.Fatal("export differs")
	}
	if invoke("export", "--store", b, "-o", dest, added.Revision) == nil {
		t.Fatal("export overwrote destination")
	}
	if e = invoke("add", "--store", a, "--object", added.Object, "--base", added.Revision, source); e != nil {
		t.Fatal(e)
	}
	if invoke("add", "--store", a, "--object", added.Object, source) == nil {
		t.Fatal("edit without base accepted")
	}
}
