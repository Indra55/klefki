package server

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	vc "klefki/core/crypto"
	"klefki/core/syncwire"
)

func TestRevisionLogAndCursor(t *testing.T) {
	s, e := Open(Config{DataDir: t.TempDir(), RateBurst: 1000})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ts := httptest.NewTLSServer(s)
	defer ts.Close()
	c := ts.Client()
	f := newFixture(t)
	session := enroll(t, c, ts.URL, f)
	other := enroll(t, c, ts.URL, newFixture(t))
	obj, _ := vc.RandomID()
	id, _ := vc.RandomID()
	meta, e := vc.EncryptMetadata(f.master, f.account.ID, obj, vc.Metadata{Type: "folder", Name: "synthetic sync folder", Tags: []string{}})
	if e != nil {
		t.Fatal(e)
	}
	rev := syncwire.Revision{ID: id.String(), Object: obj.String(), Metadata: meta}
	path := "/v1/sync/revisions/" + rev.ID
	request(t, c, ts.URL, "PUT", path, other.Token, rev, 400)
	request(t, c, ts.URL, "PUT", path, session.Token, rev, 201)
	request(t, c, ts.URL, "PUT", path, session.Token, rev, 200)
	changed := rev
	changed.Metadata = append([]byte{}, rev.Metadata...)
	changed.Metadata[len(changed.Metadata)-1] ^= 1
	request(t, c, ts.URL, "PUT", path, session.Token, changed, 409)
	var page syncwire.Page
	b := request(t, c, ts.URL, "GET", "/v1/sync/changes?after=0", session.Token, nil, 200)
	if json.Unmarshal(b, &page) != nil || len(page.Changes) != 1 {
		t.Fatal("non-idempotent log")
	}
	seq := page.Changes[0].Sequence
	request(t, c, ts.URL, "PUT", "/v1/sync/cursor", session.Token, syncwire.Cursor{Sequence: seq + 1}, 400)
	request(t, c, ts.URL, "PUT", "/v1/sync/cursor", session.Token, syncwire.Cursor{Sequence: seq}, 204)
	request(t, c, ts.URL, "PUT", "/v1/sync/cursor", session.Token, syncwire.Cursor{Sequence: 0}, 204)
	var cursor syncwire.Cursor
	b = request(t, c, ts.URL, "GET", "/v1/sync/cursor", session.Token, nil, 200)
	if json.Unmarshal(b, &cursor) != nil || cursor.Sequence != seq {
		t.Fatal("cursor moved backwards")
	}
	b = request(t, c, ts.URL, "GET", "/v1/sync/changes?after=0", other.Token, nil, 200)
	if json.Unmarshal(b, &page) != nil || len(page.Changes) != 0 {
		t.Fatal("cross-account change leak")
	}
	missing, _ := vc.RandomID()
	next, _ := vc.RandomID()
	rev.ID = next.String()
	rev.Base = missing.String()
	request(t, c, ts.URL, "PUT", "/v1/sync/revisions/"+rev.ID, session.Token, rev, 409)
	// The M2 immutable endpoint must append a revision in the same transaction.
	legacy, _ := vc.RandomID()
	m, _ := vc.EncryptMetadata(f.master, f.account.ID, legacy, vc.Metadata{Type: "folder", Name: "legacy folder", Tags: []string{}})
	request(t, c, ts.URL, "PUT", "/v1/metadata/"+legacy.String(), session.Token, Record{Metadata: m}, 201)
	b = request(t, c, ts.URL, "GET", "/v1/sync/changes?after=0", session.Token, nil, 200)
	if json.Unmarshal(b, &page) != nil || len(page.Changes) != 2 {
		t.Fatal("M2 write missing from log")
	}
}

func TestExistingM2MigrationIsIdempotent(t *testing.T) {
	root := t.TempDir()
	s, e := Open(Config{DataDir: root})
	if e != nil {
		t.Fatal(e)
	}
	f := newFixture(t)
	verifier := vc.Verifier(f.credential)
	if _, e = s.db.Exec(`INSERT INTO accounts VALUES(?,?,?,?,?)`, f.account.ID.String(), f.account.Descriptor, f.account.PasswordWrapper, f.account.RecoveryWrapper, verifier[:]); e != nil {
		t.Fatal(e)
	}
	object, _ := vc.RandomID()
	metadata, e := vc.EncryptMetadata(f.master, f.account.ID, object, vc.Metadata{Type: "folder", Name: "old M2 folder", Tags: []string{}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec(`INSERT INTO metadata VALUES(?,?,?,NULL)`, f.account.ID.String(), object.String(), metadata); e != nil {
		t.Fatal(e)
	}
	// The absent migration marker models a database written by M2.
	if _, e = s.db.Exec(`DELETE FROM sync_schema`); e != nil {
		t.Fatal(e)
	}
	s.Close()
	for i := 0; i < 2; i++ {
		s, e = Open(Config{DataDir: root})
		if e != nil {
			t.Fatal(e)
		}
		var n int
		var data []byte
		if s.db.QueryRow(`SELECT COUNT(*) FROM changes`).Scan(&n) != nil || n != 1 {
			t.Fatal("migration duplicated/lost record")
		}
		if s.db.QueryRow(`SELECT metadata FROM changes WHERE revision=?`, object.String()).Scan(&data) != nil || !bytes.Equal(data, metadata) {
			t.Fatal("migration changed ciphertext")
		}
		s.Close()
	}
}
