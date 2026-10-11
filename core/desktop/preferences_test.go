package desktop

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLibraryPreferencesPersistAcrossRestart(t *testing.T) {
	root := t.TempDir()
	s, err := New(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	want := Preferences{Theme: "light", IdleMinutes: 15, Density: "compact", DefaultSort: "size"}
	if err := s.Preferences(want); err != nil {
		t.Fatal(err)
	}
	s.Close()
	reopened, err := New(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	if got := reopened.State().Config.Preferences; got != want {
		t.Fatalf("preferences after restart: got %+v, want %+v", got, want)
	}
	for _, invalid := range []Preferences{
		{Theme: "light", IdleMinutes: 15, Density: "dense", DefaultSort: "name"},
		{Theme: "light", IdleMinutes: 15, Density: "comfortable", DefaultSort: "date"},
	} {
		if err := reopened.Preferences(invalid); err != ErrInvalid {
			t.Fatalf("invalid preferences accepted: %v", err)
		}
		if reopened.State().Config.Preferences != want {
			t.Fatal("rejected preferences changed saved state")
		}
	}
}

func TestOlderSettingsRetainThemeAndLockPreferences(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "settings.json"), []byte(`{"preferences":{"theme":"system","idleMinutes":30}}`), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := New(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	want := Preferences{Theme: "system", IdleMinutes: 30, Density: "comfortable", DefaultSort: "name"}
	if got := s.State().Config.Preferences; got != want {
		t.Fatalf("older settings changed: got %+v, want %+v", got, want)
	}
	if err := s.Preferences(Preferences{Theme: "light", IdleMinutes: 5}); err != nil {
		t.Fatal(err)
	}
	if got := s.State().Config.Preferences; got.Density != "comfortable" || got.DefaultSort != "name" {
		t.Fatal("missing library preferences did not receive defaults")
	}
}

func TestFailedPreferenceSaveKeepsPreviousPreferences(t *testing.T) {
	s, err := New(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	want := s.State().Config.Preferences
	s.configPath = t.TempDir()
	if err := s.Preferences(Preferences{Theme: "light", IdleMinutes: 15, Density: "compact", DefaultSort: "size"}); err == nil {
		t.Fatal("saving settings over a directory succeeded")
	}
	if s.State().Config.Preferences != want {
		t.Fatal("failed save changed active preferences")
	}
}
