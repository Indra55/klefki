package desktop

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
	"vault/core/search"
	vs "vault/core/sync"
)

func TestDesktopSearchRebuildAndLock(t *testing.T) {
	s, _ := testService(t)
	revision := put(t, s, "document.txt", nil)
	events := make(chan Event, 16)
	s.emit = func(e Event) { events <- e }
	op, e := s.RebuildIndex()
	if e != nil {
		t.Fatal(e)
	}
	timeout := time.After(5 * time.Second)
done:
	for {
		select {
		case event := <-events:
			if event.Kind == "finished" && event.Operation == op {
				if event.Error != "" {
					t.Fatal(event.Error)
				}
				break done
			}
		case <-timeout:
			t.Fatal("index timed out")
		}
	}
	result, e := s.Search("body")
	if e != nil || len(result.Hits) != 1 || result.Hits[0].Revision != revision {
		t.Fatal("content search", e)
	}
	list, e := s.List()
	if e != nil || len(list.Items) != 1 {
		t.Fatal("internal index visible", e)
	}
	if _, e = s.Edit(revision, vs.Edit{Name: "new.txt", Tags: []string{}}); e != nil {
		t.Fatal(e)
	}
	if result, e = s.Search("body"); e != nil || len(result.Hits) != 1 || result.Hits[0].Name != "new.txt" {
		t.Fatal("automatic rebuild after edit", e)
	}
	s.Lock()
	result, e = s.Search("body")
	if !errors.Is(e, ErrLocked) || len(result.Hits) != 0 {
		t.Fatal("locked search", e)
	}
	if _, e = s.RebuildIndex(); !errors.Is(e, ErrLocked) {
		t.Fatal("locked rebuild", e)
	}
}

func TestImportScanThenSearchWithoutManualRebuild(t *testing.T) {
	if _, e := exec.LookPath("tesseract"); e != nil {
		t.Skip("Tesseract unavailable")
	}
	s, _ := testService(t)
	events := make(chan Event, 32)
	s.emit = func(e Event) { events <- e }
	path, e := filepath.Abs("../search/testdata/scan.png")
	if e != nil {
		t.Fatal(e)
	}
	op, e := s.Import([]string{path}, nil, []string{})
	if e != nil {
		t.Fatal(e)
	}
	timeout := time.After(10 * time.Second)
	started, indexed := false, false
done:
	for {
		select {
		case event := <-events:
			if event.Operation != op {
				continue
			}
			if event.Kind == "index-start" {
				started = true
			}
			if event.Kind == "index-finished" {
				if !started || event.Error != "" || event.DurationMS <= 0 {
					t.Fatalf("invalid indexing completion: %+v", event)
				}
				indexed = true
			}
			if event.Kind == "finished" && event.Operation == op {
				if event.Error != "" {
					t.Fatal(event.Error)
				}
				break done
			}
		case <-timeout:
			t.Fatal("import timed out")
		}
	}
	if !indexed {
		t.Fatal("upload finished without indexing")
	}
	// Query the core directly so search's fallback cannot hide a missing upload index.
	if e = s.with(func(x *session) error {
		hits, e := search.Query(x.ctx, x.store, x.master, "nebula")
		if e == nil && (len(hits) != 1 || hits[0].Name != "scan.png") {
			t.Fatal("uploaded scan not searchable")
		}
		return e
	}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Search("certificate"); e != nil {
		t.Fatal("second search", e)
	}
	list, e := s.List()
	if e != nil || len(list.Items) != 1 {
		t.Fatal("index visible", e)
	}
}

func TestImportBatchIndexesSavedFilesOnce(t *testing.T) {
	s, _ := testService(t)
	events := make(chan Event, 32)
	s.emit = func(e Event) { events <- e }
	root := t.TempDir()
	paths := []string{filepath.Join(root, "one.txt"), filepath.Join(root, "missing.txt"), filepath.Join(root, "two.txt")}
	for _, path := range []string{paths[0], paths[2]} {
		if e := os.WriteFile(path, []byte("quasar"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	op, e := s.Import(paths, nil, []string{})
	if e != nil {
		t.Fatal(e)
	}
	starts, completions := 0, 0
	for _, event := range importEvents(t, events, op) {
		switch event.Kind {
		case "index-start":
			starts++
		case "index-finished":
			if event.Error != "" || event.DurationMS <= 0 {
				t.Fatalf("invalid indexing completion: %+v", event)
			}
			completions++
		case "finished":
			if event.Error != "IMPORT_PARTIAL" {
				t.Fatal("missing partial import error", event.Error)
			}
		}
	}
	if starts != 1 || completions != 1 {
		t.Fatal("expected one rebuild per batch", starts, completions)
	}
	if e := s.with(func(x *session) error {
		hits, e := search.Query(x.ctx, x.store, x.master, "quasar")
		if e == nil && len(hits) != 2 {
			t.Fatal("saved documents missing from index", len(hits))
		}
		return e
	}); e != nil {
		t.Fatal(e)
	}
}

func TestImportIndexFailureAndCancellationKeepDocument(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		name := "extraction failure"
		if cancel {
			name = "cancellation"
		}
		t.Run(name, func(t *testing.T) {
			s, _ := testService(t)
			events := make(chan Event, 32)
			s.emit = func(e Event) {
				if cancel && e.Kind == "index-start" {
					s.Cancel(e.Operation)
				}
				events <- e
			}
			path := filepath.Join(t.TempDir(), "invalid.png")
			if e := os.WriteFile(path, []byte("invalid image"), 0600); e != nil {
				t.Fatal(e)
			}
			op, e := s.Import([]string{path}, nil, []string{})
			if e != nil {
				t.Fatal(e)
			}
			completed := false
			for _, event := range importEvents(t, events, op) {
				if event.Kind == "index-finished" || event.Kind == "finished" {
					if event.Error == "" || (cancel && event.Error != "CANCELED") {
						t.Fatalf("unexpected success: %+v", event)
					}
					completed = completed || event.Kind == "index-finished"
				}
			}
			if !completed {
				t.Fatal("missing indexing result")
			}
			list, e := s.List()
			if e != nil || len(list.Items) != 1 || list.Items[0].Name != "invalid.png" {
				t.Fatal("saved document lost", e)
			}
		})
	}
}

func importEvents(t *testing.T, events <-chan Event, op string) []Event {
	t.Helper()
	var result []Event
	timeout := time.After(10 * time.Second)
	for {
		select {
		case event := <-events:
			if event.Operation != op {
				continue
			}
			result = append(result, event)
			if event.Kind == "finished" {
				return result
			}
		case <-timeout:
			t.Fatal("import timed out")
		}
	}
}
