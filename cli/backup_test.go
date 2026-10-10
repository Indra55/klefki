package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	vc "klefki/core/crypto"
	"klefki/core/local"
	vs "klefki/core/sync"
)

func TestCLIBackupRestore(t *testing.T) {
	root := t.TempDir()
	password := filepath.Join(root, "password")
	recovery := filepath.Join(root, "recovery")
	accountPath := filepath.Join(root, "account.v1")
	if e := os.WriteFile(password, []byte("public backup fixture password"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := run([]string{"init", "--klefki", accountPath, "--password-file", password, "--recovery-out", recovery}, io.Discard); e != nil {
		t.Fatal(e)
	}
	account, e := local.LoadAccount(accountPath)
	if e != nil {
		t.Fatal(e)
	}
	master, e := account.Unlock([]byte("public backup fixture password"))
	if e != nil {
		t.Fatal(e)
	}
	defer vc.Wipe(master[:])
	s, e := vs.Open(filepath.Join(root, "local.db"), account.ID)
	if e != nil {
		t.Fatal(e)
	}
	folder, _ := vc.RandomID()
	object, _ := vc.RandomID()
	parent := folder.String()
	if _, e = s.Put(master, parent, "", vc.Metadata{Type: "folder", Name: "private-folder", Tags: []string{"private-tag"}}, nil); e != nil {
		t.Fatal(e)
	}
	body := bytes.Repeat([]byte("synthetic plaintext backup fixture"), 140000)
	first, e := s.Put(master, object.String(), "", vc.Metadata{Type: "file", Name: "duplicate.txt", Parent: &parent, Tags: []string{}, Size: uint64(len(body))}, bytes.NewReader(body))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Edit(master, first, vs.Edit{Name: "duplicate.txt", Parent: &parent, Tags: []string{"klefki:system:trash"}, Favorite: true}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Put(master, object.String(), first, vc.Metadata{Type: "file", Name: "duplicate.txt", Parent: &parent, Tags: []string{}}, bytes.NewReader(nil)); e != nil {
		t.Fatal(e)
	}
	before, e := s.Check(master)
	if e != nil {
		t.Fatal(e)
	}
	heads, e := s.Heads(master)
	if e != nil {
		t.Fatal(e)
	}
	s.Close()
	invoke := func(command, account, dest string, extra ...string) error {
		args := []string{command, "--klefki", account, "--password-file", password}
		if dest != "" {
			args = append(args, "-o", dest)
		}
		return run(append(args, extra...), io.Discard)
	}
	backup := filepath.Join(root, "backup")
	if e = invoke("backup", accountPath, backup); e != nil {
		t.Fatal(e)
	}
	if e = invoke("backup", accountPath, backup); e == nil {
		t.Fatal("existing output replaced")
	}
	// Only the exported files and externally supplied credentials enter each new directory.
	for _, credential := range []struct{ flag, path string }{{"--password-file", password}, {"--recovery-file", recovery}} {
		restored := filepath.Join(t.TempDir(), "restored")
		if e = run([]string{"restore", credential.flag, credential.path, "-o", restored, backup}, io.Discard); e != nil {
			t.Fatal(e)
		}
		restoredAccount := filepath.Join(restored, "account.v1")
		original, _ := os.ReadFile(accountPath)
		got, _ := os.ReadFile(restoredAccount)
		if !bytes.Equal(original, got) {
			t.Fatal("account differs")
		}
		restoredStore, e := vs.OpenExisting(filepath.Join(restored, "local.db"), account.ID)
		if e != nil {
			t.Fatal(e)
		}
		after, e := restoredStore.Check(master)
		if e != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("metadata/history differs", e)
		}
		gotHeads, e := restoredStore.Heads(master)
		if e != nil || !reflect.DeepEqual(heads, gotHeads) {
			t.Fatal("conflicts differ", e)
		}
		pending, _ := restoredStore.Pending()
		if pending != len(before) {
			t.Fatal("queue differs")
		}
		restoredStore.Close()
		if e = invoke("check", restoredAccount, ""); e != nil {
			t.Fatal(e)
		}
		plain := filepath.Join(t.TempDir(), "plain")
		if e = invoke("export-plain", restoredAccount, plain); e != nil {
			t.Fatal(e)
		}
		manifest, e := os.ReadFile(filepath.Join(plain, "manifest.json"))
		if e != nil {
			t.Fatal(e)
		}
		var parsed struct {
			Revisions []vs.Revision `json:"revisions"`
		}
		if json.Unmarshal(manifest, &parsed) != nil || !reflect.DeepEqual(before, parsed.Revisions) {
			t.Fatal("manifest differs")
		}
		for _, r := range before {
			if r.Metadata.Type != "file" {
				continue
			}
			data, e := os.ReadFile(filepath.Join(plain, r.Revision))
			if e != nil {
				t.Fatal(e)
			}
			expected := body
			if r.Metadata.Size == 0 {
				expected = nil
			}
			if !bytes.Equal(data, expected) {
				t.Fatal("plaintext differs")
			}
		}
	}
	for _, name := range []string{"account.v1", "local.db"} {
		data, e := os.ReadFile(filepath.Join(backup, name))
		if e != nil {
			t.Fatal(e)
		}
		for _, secret := range []string{"synthetic plaintext backup fixture", "duplicate.txt", "private-folder", "private-tag", "public backup fixture password"} {
			if bytes.Contains(data, []byte(secret)) {
				t.Fatal("plaintext in encrypted export")
			}
		}
	}
	// A malformed backup must leave neither a successful destination nor changes to its input.
	dbPath := filepath.Join(backup, "local.db")
	if e = os.WriteFile(dbPath, []byte("invalid database"), 0600); e != nil {
		t.Fatal(e)
	}
	failed := filepath.Join(root, "failed")
	if e = run([]string{"restore", "--password-file", password, "-o", failed, backup}, io.Discard); e == nil {
		t.Fatal("invalid backup accepted")
	}
	if _, e = os.Stat(failed); !os.IsNotExist(e) {
		t.Fatal("partial restore left behind")
	}
	if e = invoke("backup", accountPath, filepath.Join(root, "missing"), "--store", filepath.Join(root, "absent.db")); e == nil {
		t.Fatal("missing store backed up")
	}
}
