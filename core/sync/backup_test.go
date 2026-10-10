package sync

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	vc "klefki/core/crypto"
)

func TestBackupRoundTripAndIntegrity(t *testing.T) {
	account, _ := vc.RandomID()
	master, _ := vc.RandomKey()
	defer vc.Wipe(master[:])
	s, e := Open(filepath.Join(t.TempDir(), "source.db"), account)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	object, _ := vc.RandomID()
	body := bytes.Repeat([]byte("synthetic backup data"), vc.ChunkSize/20+1)
	first, e := s.Put(master, object.String(), "", vc.Metadata{Type: "file", Name: "private.txt", Tags: []string{}, Size: uint64(len(body))}, bytes.NewReader(body))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Edit(master, first, Edit{Name: "renamed.txt", Tags: []string{"klefki:system:trash"}, Favorite: true}); e != nil {
		t.Fatal(e)
	}
	folder, _ := vc.RandomID()
	if _, e = s.Put(master, folder.String(), "", vc.Metadata{Type: "folder", Name: "folder", Tags: []string{}}, nil); e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec(`UPDATE local_state SET remote='https://example.invalid',cursor=7`); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(t.TempDir(), "copy.db")
	if e = s.Snapshot(path); e != nil {
		t.Fatal(e)
	}
	if e = s.Snapshot(path); e == nil {
		t.Fatal("replaced snapshot")
	}
	copied, e := Open(path, account)
	if e != nil {
		t.Fatal(e)
	}
	defer copied.Close()
	before, e := s.Check(master)
	if e != nil {
		t.Fatal(e)
	}
	after, e := copied.Check(master)
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, after) || len(after) != 3 {
		t.Fatal("revisions differ")
	}
	cursor, _ := copied.Cursor()
	pending, _ := copied.Pending()
	if cursor != 7 || pending != 3 {
		t.Fatal("sync state lost")
	}
	var out bytes.Buffer
	if e = copied.Export(master, first, &out); e != nil || !bytes.Equal(body, out.Bytes()) {
		t.Fatal("content differs", e)
	}
	wrong, _ := vc.RandomKey()
	if _, e = copied.Check(wrong); e == nil {
		t.Fatal("wrong key accepted")
	}
	if _, e = copied.db.Exec(`UPDATE chunks SET data=zeroblob(length(data)) WHERE idx=1`); e != nil {
		t.Fatal(e)
	}
	if _, e = copied.Check(master); e == nil {
		t.Fatal("tamper accepted")
	}
}

func TestOpenExistingRefusesMissingAndEmptyStore(t *testing.T) {
	account, _ := vc.RandomID()
	path := filepath.Join(t.TempDir(), "local.db")
	if s, e := OpenExisting(path, account); e == nil {
		s.Close()
		t.Fatal("created missing store")
	}
	if _, e := os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("missing store created")
	}
	if e := os.WriteFile(path, nil, 0600); e != nil {
		t.Fatal(e)
	}
	if s, e := OpenExisting(path, account); e == nil {
		s.Close()
		t.Fatal("initialized empty file")
	}
	st, e := os.Stat(path)
	if e != nil || st.Size() != 0 {
		t.Fatal("empty input changed")
	}
}

func TestCheckRejectsIncompleteStore(t *testing.T) {
	for _, mutation := range []string{
		`DELETE FROM chunks`,
		`UPDATE chunks SET idx=3`,
		`UPDATE revisions SET base=id`,
		`UPDATE revisions SET metadata=zeroblob(length(metadata))`,
		`INSERT INTO chunks SELECT 'ffffffffffffffffffffffffffffffff',idx,data FROM chunks`,
	} {
		t.Run(mutation, func(t *testing.T) {
			account, _ := vc.RandomID()
			master, _ := vc.RandomKey()
			defer vc.Wipe(master[:])
			s, e := Open(filepath.Join(t.TempDir(), "local.db"), account)
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			object, _ := vc.RandomID()
			if _, e = s.Put(master, object.String(), "", vc.Metadata{Type: "file", Name: "empty", Tags: []string{}}, bytes.NewReader(nil)); e != nil {
				t.Fatal(e)
			}
			if _, e = s.db.Exec(mutation); e != nil {
				t.Fatal(e)
			}
			if _, e = s.Check(master); e == nil {
				t.Fatal("invalid store accepted")
			}
		})
	}
}
