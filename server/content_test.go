package server

import (
	"context"
	"net/http/httptest"
	"testing"

	vc "klefki/core/crypto"
	"klefki/core/syncwire"
)

func TestContentMigrationAdoptsLegacyFilesOnce(t *testing.T) {
	s, err := Open(Config{DataDir: t.TempDir(), RateBurst: 1000})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	web := httptest.NewTLSServer(s)
	defer web.Close()
	fixture := newFixture(t)
	auth := enroll(t, web.Client(), web.URL, fixture)
	file, _ := vc.RandomID()
	object, _ := vc.RandomID()
	key, _ := vc.RandomKey()
	chunk, _ := vc.Seal(key, vc.Context{Kind: vc.Chunk, Account: fixture.account.ID, Object: file, Final: true}, []byte("legacy bytes"), nil)
	request(t, web.Client(), web.URL, "PUT", "/v1/blobs/"+file.String()+"/chunks/0", auth.Token, chunk, 201)
	metadata, _ := vc.EncryptMetadata(fixture.master, fixture.account.ID, object, vc.Metadata{Type: "file", Name: "legacy.txt", Tags: []string{}, FileID: file.String(), Size: 12, ChunkCount: 1})
	wrapped, _ := vc.Seal(fixture.master, vc.Context{Kind: vc.FileKey, Account: fixture.account.ID, Object: file}, key[:], nil)
	revision := syncwire.Revision{ID: object.String(), Object: object.String(), Metadata: metadata, FileKey: wrapped}
	request(t, web.Client(), web.URL, "PUT", "/v1/sync/revisions/"+revision.ID, auth.Token, revision, 201)
	if _, err = s.db.Exec(`DROP TABLE content_schema;DELETE FROM content_available;DELETE FROM relay_revisions`); err != nil {
		t.Fatal(err)
	}
	if err = migrateContent(s.db); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM content_available WHERE account=? AND file=?`, fixture.account.ID.String(), file.String()).Scan(&n); err != nil || n != 1 {
		t.Fatal("legacy contents not adopted", n, err)
	}
	// A new selective publication deliberately has metadata without file chunks.
	// Subsequent startups must not reinterpret it as a legacy completed upload.
	nextFile, _ := vc.RandomID()
	nextObject, _ := vc.RandomID()
	revision.ID = nextObject.String()
	revision.Object = revision.ID
	revision.Metadata, _ = vc.EncryptMetadata(fixture.master, fixture.account.ID, nextObject, vc.Metadata{Type: "file", Name: "deferred.txt", Tags: []string{}, FileID: nextFile.String(), Size: 12, ChunkCount: 1})
	revision.FileKey, _ = vc.Seal(fixture.master, vc.Context{Kind: vc.FileKey, Account: fixture.account.ID, Object: nextFile}, key[:], nil)
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = appendChange(context.Background(), tx, fixture.account.ID.String(), revision); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = migrateContent(s.db); err != nil {
		t.Fatal(err)
	}
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM content_available WHERE file=?`, nextFile.String()).Scan(&n); err != nil || n != 0 {
		t.Fatal("deferred upload falsely marked available", n, err)
	}
}
