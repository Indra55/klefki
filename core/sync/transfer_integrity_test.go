package sync

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	vc "klefki/core/crypto"
)

func TestTransferCopyFailureIsAtomic(t *testing.T) {
	for _, failure := range []string{"corrupt", "unavailable", "cancelled"} {
		t.Run(failure, func(t *testing.T) {
			account, _ := vc.RandomID()
			master, _ := vc.RandomKey()
			defer vc.Wipe(master[:])
			s, e := Open(filepath.Join(t.TempDir(), "store.db"), account)
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			folder, _ := vc.RandomID()
			file, _ := vc.RandomID()
			copyFolder, _ := vc.RandomID()
			copyFile, _ := vc.RandomID()
			first, e := s.Put(master, folder.String(), "", vc.Metadata{Type: "folder", Name: "Empty", Tags: []string{}}, nil)
			if e != nil {
				t.Fatal(e)
			}
			body := []byte("synthetic transfer integrity content")
			second, e := s.Put(master, file.String(), "", vc.Metadata{Type: "file", Name: "file.txt", Tags: []string{}, Size: uint64(len(body))}, bytes.NewReader(body))
			if e != nil {
				t.Fatal(e)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch failure {
			case "corrupt":
				_, e = s.db.Exec(`UPDATE chunks SET data=zeroblob(length(data))`)
			case "unavailable":
				if _, e = s.db.Exec(`DELETE FROM chunks`); e == nil {
					_, e = s.db.Exec(`INSERT INTO deferred_content(revision,trash) VALUES(?,0)`, second)
				}
			case "cancelled":
				cancel()
			}
			if e != nil {
				t.Fatal(e)
			}
			var before int
			if e = s.db.QueryRow(`SELECT COUNT(*) FROM chunks`).Scan(&before); e != nil {
				t.Fatal(e)
			}
			snapshot, e := s.AllHeads(master)
			if e != nil {
				t.Fatal(e)
			}
			if e = s.TransferContext(ctx, master, snapshot, []TransferChange{{Revision: first, Object: copyFolder.String()}, {Revision: second, Object: copyFile.String()}}, true); e == nil {
				t.Fatal("failed copy accepted")
			}
			heads, e := s.AllHeads(master)
			if e != nil || len(heads) != 2 {
				t.Fatal("partial copy left behind", len(heads), e)
			}
			var after int
			if e = s.db.QueryRow(`SELECT COUNT(*) FROM chunks`).Scan(&after); e != nil || after != before {
				t.Fatal("partial encrypted chunks left behind", e)
			}
		})
	}
}
