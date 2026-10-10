package desktop

import (
	"errors"
	"os"
	"testing"
	"time"

	"klefki/core/semantic"
)

func TestDesktopM8DefaultOffSetupSearchAndDisable(t *testing.T) {
	root := os.Getenv("KLEFKI_MODEL_DIR")
	if root == "" || !semantic.Supported() {
		t.Skip("requires native model")
	}
	s, _ := testService(t)
	rev := put(t, s, "document.txt", nil)
	status, e := s.SemanticStatus()
	if e != nil || status.Enabled {
		t.Fatal("M8 not default off", e)
	}
	if _, e = s.Analyze(rev); e == nil {
		t.Fatal("analysis without opt in")
	}
	events := make(chan Event, 64)
	s.emit = func(e Event) { events <- e }
	op, e := s.SetupSemantic(root)
	if e != nil {
		t.Fatal(e)
	}
	timeout := time.After(20 * time.Second)
done:
	for {
		select {
		case event := <-events:
			if event.Kind == "finished" && event.Operation == op {
				if event.Error != "" {
					t.Fatal("setup", event.Error)
				}
				break done
			}
		case <-timeout:
			t.Fatal("setup timed out")
		}
	}
	result, e := s.Search("body")
	if e != nil || result.Mode != "hybrid" {
		t.Fatal("desktop hybrid", e)
	}
	a, e := s.Analyze(rev)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.CorrectAnalysis(rev, "Work", a.Fields, nil); e != nil {
		t.Fatal(e)
	}
	list, e := s.List()
	if e != nil || len(list.Items) != 1 || list.Items[0].Category != "Work" {
		t.Fatal("category overlay or internal files", e)
	}
	if e = s.ConfigureSemantic(SemanticPreferences{Thresholds: semantic.DefaultThresholds}); e != nil {
		t.Fatal(e)
	}
	result, e = s.Search("body")
	if e != nil || result.Mode != "keyword" || result.Availability != "disabled" {
		t.Fatal("disabled mode", e)
	}
	s.Lock()
	if _, e = s.Analyze(rev); !errors.Is(e, ErrLocked) {
		t.Fatal("analysis after lock", e)
	}
}

func TestSemanticPreferencesMigrateWithModel(t *testing.T) {
	s, _ := testService(t)
	s.mu.Lock()
	binding := accountBinding(s.session.account)
	s.config.Semantic = map[string]SemanticPreferences{binding: {Enabled: true, Thresholds: semantic.Thresholds{Cutoff: .2}, ModelFingerprint: "legacy-minilm"}}
	s.mu.Unlock()
	status, err := s.SemanticStatus()
	if err != nil || !status.Enabled || status.Thresholds != semantic.DefaultThresholds || status.ModelFingerprint != semantic.Fingerprint() {
		t.Fatal("model defaults did not migrate while preserving opt-in", status, err)
	}
	custom := semantic.Thresholds{Cutoff: .75, Margin: .08}
	if err := s.ConfigureSemantic(SemanticPreferences{Thresholds: custom}); err != nil {
		t.Fatal(err)
	}
	status, err = s.SemanticStatus()
	if err != nil || status.Enabled || status.Thresholds != custom {
		t.Fatal("current model preferences were not retained", status, err)
	}
}
