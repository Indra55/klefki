package desktop

import (
	"archive/zip"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func exportSelectionResult(t *testing.T, s *Service, revisions []string, path string) string {
	t.Helper()
	events := make(chan Event, 32)
	s.emit = func(e Event) { events <- e }
	op, err := s.ExportSelection(revisions, path)
	if err != nil {
		return Code(err)
	}
	result := importEvents(t, events, op)
	return result[len(result)-1].Error
}

func TestExportSelectionZIPPreservesContentsFoldersAndDuplicateNames(t *testing.T) {
	s, _ := testService(t)
	folder, _ := s.Folder("Records", nil)
	f := find(t, s, folder)
	_, _ = s.Folder("Empty", &f.Object)
	put(t, s, "Résumé.txt", &f.Object)
	a := put(t, s, "report.txt", nil)
	b := put(t, s, "REPORT.txt", nil)
	before, _ := s.List()
	path := filepath.Join(t.TempDir(), "documents.zip")
	if code := exportSelectionResult(t, s, []string{folder, a, b}, path); code != "" {
		t.Fatal(code)
	}
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	got := map[string]string{}
	for _, file := range reader.File {
		r, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		got[file.Name] = string(data)
	}
	want := map[string]string{"Records/": "", "Records/Empty/": "", "Records/Résumé.txt": "private-body", "report.txt": "private-body", "REPORT (2).txt": "private-body"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("archive contents: %#v", got)
	}
	after, _ := s.List()
	if !reflect.DeepEqual(before, after) {
		t.Fatal("export modified the library")
	}
	if code := exportSelectionResult(t, s, []string{a}, path); code != "EXISTS" {
		t.Fatal("overwrote archive", code)
	}
}

func TestExportSelectionFailurePublishesNoPartialArchive(t *testing.T) {
	s, _ := testService(t)
	folder, _ := s.Folder("Empty", nil)
	rev := put(t, s, "damaged.txt", nil)
	db, err := sql.Open("sqlite", s.State().Config.StorePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`UPDATE chunks SET data=zeroblob(length(data))`); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "documents.zip")
	if code := exportSelectionResult(t, s, []string{folder, rev}, path); code == "" {
		t.Fatal("corrupt export succeeded")
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("partial archive published", err)
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 0 {
		t.Fatal("staged plaintext left behind", files, err)
	}
}

func TestExportSelectionRejectsStaleMixedDirectoriesAndUnsafePaths(t *testing.T) {
	s, _ := testService(t)
	folder, _ := s.Folder("Records", nil)
	f := find(t, s, folder)
	inside := put(t, s, "inside.txt", &f.Object)
	for _, revisions := range [][]string{nil, {folder, inside}, {inside, inside}, {"missing"}} {
		path := filepath.Join(t.TempDir(), "documents.zip")
		if code := exportSelectionResult(t, s, revisions, path); code == "" {
			t.Fatal("invalid selection accepted", revisions)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("invalid selection published", err)
		}
	}
	entries, err := exportEntries([]Item{{Object: "a", Revision: "a", Name: "..", Type: "folder"}, {Object: "b", Revision: "b", Name: "CON.txt", Type: "file"}}, []string{"a", "b"})
	if err != nil || len(entries) != 2 || entries[0].name != "Untitled/" || entries[1].name != "_CON.txt" {
		t.Fatal("unsafe paths", entries, err)
	}
}

func TestExportSelectionCancellationRemovesStagedArchive(t *testing.T) {
	s, _ := testService(t)
	folder, _ := s.Folder("Empty", nil)
	rev := put(t, s, "inside.txt", nil)
	events := make(chan Event, 32)
	s.emit = func(e Event) {
		if e.Kind == "progress" {
			s.Cancel(e.Operation)
		}
		events <- e
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "documents.zip")
	op, err := s.ExportSelection([]string{folder, rev}, path)
	if err != nil {
		t.Fatal(err)
	}
	result := importEvents(t, events, op)
	if code := result[len(result)-1].Error; code != "CANCELED" {
		t.Fatal("cancellation ignored", code)
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 0 {
		t.Fatal("canceled archive left behind", files, err)
	}
}
