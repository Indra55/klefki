package search

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	vc "klefki/core/crypto"
	"klefki/core/semantic"
	vs "klefki/core/sync"
)

func axis(n int) []float32 {
	v := make([]float32, semantic.Dimension)
	v[n] = 1
	return v
}

func TestOrganizationCategorySectionsAndAmbiguity(t *testing.T) {
	candidates := make([]tagCandidate, len(semantic.Labels))
	for i, label := range semantic.Labels {
		candidates[i] = tagCandidate{name: label.Name, vector: axis(i)}
	}
	section := func(category, other int, score float64) []float32 {
		v := axis(other)
		v[category], v[other] = float32(score), float32(math.Sqrt(1-score*score))
		return v
	}
	for _, tc := range []struct {
		name   string
		chunks [][]float32
		want   int
	}{
		{"company background does not drown out hiring section", [][]float32{section(1, 10, .49), section(1, 11, .49), section(1, 12, .49), section(5, 13, .55)}, 5},
		{"conflicting confident sections abstain", [][]float32{axis(1), axis(5)}, -1},
		{"consistent weaker sections retain aggregate evidence", [][]float32{section(0, 10, .49), section(0, 11, .49)}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := SemanticDocument{Chunks: tc.chunks, Vector: semantic.Mean(tc.chunks)}
			winner, _, confident := winningTag(categoryTagScores(d, candidates, semantic.DefaultThresholds), semantic.DefaultThresholds)
			if tc.want < 0 {
				if confident {
					t.Fatal("ambiguous document forced into a category", candidates[winner].name)
				}
			} else if !confident || winner != tc.want {
				t.Fatal("incorrect category", winner, confident)
			}
		})
	}
}

type tagEmbedder struct{ testEmbedder }

func (tagEmbedder) EmbedTokens(ctx context.Context, tokens []int64) ([]float32, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	runes := []rune{}
	for _, token := range tokens {
		runes = append(runes, rune(token))
	}
	if strings.Contains(string(runes), "Recipes") {
		return axis(7), nil
	}
	if strings.Contains(string(runes), "Job descriptions and job postings") {
		return axis(5), nil
	}
	// Astronomy only matches through a previously tagged document.
	return axis(10), nil
}

func TestSuggestTagsMatchesCustomLearnsTagsAndGroupsUnmatched(t *testing.T) {
	account, _ := vc.RandomID()
	key, _ := vc.RandomKey()
	defer vc.Wipe(key[:])
	s, e := vs.Open(filepath.Join(t.TempDir(), "local.db"), account)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	type fixture struct {
		name, body string
		tags       []string
		axis       int
	}
	fixtures := []fixture{
		{"resume.txt", "software engineer skills experience", []string{"identity"}, 5},
		{"job description.txt", "software engineer responsibilities qualifications", []string{}, 5},
		{"observatory.txt", "astronomy telescope stars observatory", []string{"Astronomy"}, 6},
		{"nebula.txt", "astronomy telescope stars nebula", []string{}, 6},
		{"cooking.txt", "recipe vegetables cooking soup", []string{}, 7},
		{"jazz concert.txt", "jazz music saxophone concert", []string{}, 8},
		{"jazz rehearsal.txt", "jazz music saxophone rehearsal", []string{}, 8},
		{"rail itinerary.txt", "rail travel itinerary station", []string{}, 9},
		{"empty.dat", "", []string{}, -1},
	}
	axes := map[string]int{}
	revisions := map[string]string{}
	for _, f := range fixtures {
		id, _ := vc.RandomID()
		rev, e := s.Put(key, id.String(), "", vc.Metadata{Type: "file", Name: f.name, Tags: f.tags, Size: uint64(len(f.body))}, strings.NewReader(f.body))
		if e != nil {
			t.Fatal(e)
		}
		axes[rev], revisions[f.name] = f.axis, rev
	}
	ctx := context.Background()
	if _, e := BuildSemantic(ctx, s, key, Extractor{}, testEmbedder{}); e != nil {
		t.Fatal(e)
	}
	heads, _ := s.Heads(key)
	docs := documents(heads)
	ix, e := loadSemantic(ctx, s, key, heads, docs, semantic.Fingerprint())
	if e != nil {
		t.Fatal(e)
	}
	defer ix.Clear()
	for i := range ix.Labels {
		ix.Labels[i] = axis(i)
	}
	for i := range ix.Documents {
		d := &ix.Documents[i]
		if n := axes[d.Revision]; n >= 0 {
			d.Vector, d.Chunks = axis(n), [][]float32{axis(n)}
		}
	}
	if _, e := publishSidecar(ctx, s, key, heads, SemanticTag, "semantic-index.v1.json", ix); e != nil {
		t.Fatal(e)
	}
	pending, _ := s.Pending()
	thresholds := semantic.Thresholds{Cutoff: .8, Margin: .05}
	plan, e := SuggestTags(ctx, s, key, tagEmbedder{}, []string{"Recipes"}, thresholds)
	if e != nil || len(plan.Suggestions) != 7 {
		t.Fatal(plan, e)
	}
	byName := map[string]TagSuggestion{}
	for _, row := range plan.Suggestions {
		byName[row.Name] = row
		if !vs.ValidUserTag(row.Tag) {
			t.Fatal("invalid proposed tag", row)
		}
	}
	if row := byName["nebula.txt"]; row.Tag != "Astronomy" || row.New {
		t.Fatal("existing tag examples ignored", row)
	}
	if row := byName["job description.txt"]; row.Tag != "Work" || row.New {
		t.Fatal("resume tag changed the meaning of a built-in category", row)
	}
	if row := byName["cooking.txt"]; row.Tag != "Recipes" || row.New {
		t.Fatal("custom tag ignored", row)
	}
	music := byName["jazz concert.txt"]
	if !music.New || music.Tag != byName["jazz rehearsal.txt"].Tag || !strings.Contains(music.Tag, "Jazz") {
		t.Fatal("related documents were not grouped with a content name", plan)
	}
	if byName["rail itinerary.txt"].Tag == music.Tag || !byName["rail itinerary.txt"].New {
		t.Fatal("unrelated document joined group")
	}
	if plan.Skipped != 2 {
		t.Fatal("already tagged and empty file not skipped", plan)
	}
	if after, _ := s.Pending(); after != pending {
		t.Fatal("preview changed store")
	}
	repeat, e := SuggestTags(ctx, s, key, tagEmbedder{}, []string{"Recipes"}, thresholds)
	if e != nil || !reflect.DeepEqual(plan, repeat) {
		t.Fatal("non-deterministic suggestions", e)
	}
	if _, e := SuggestTags(ctx, s, key, tagEmbedder{}, []string{"klefki:system:bad"}, thresholds); e == nil {
		t.Fatal("reserved custom tag accepted")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, e := SuggestTags(cancelled, s, key, tagEmbedder{}, nil, thresholds); !errors.Is(e, context.Canceled) {
		t.Fatal("cancellation ignored", e)
	}
	if _, e := SaveCorrection(ctx, s, key, revisions["cooking.txt"], "Money", nil, nil); e != nil {
		t.Fatal(e)
	}
	plan, e = SuggestTags(ctx, s, key, tagEmbedder{}, []string{"Recipes"}, thresholds)
	if e != nil {
		t.Fatal(e)
	}
	for _, row := range plan.Suggestions {
		if row.Name == "cooking.txt" && (row.Tag != "Money" || row.Basis != "confirmed category") {
			t.Fatal("confirmed category overwritten", row)
		}
	}
	if _, e := s.Edit(key, revisions["nebula.txt"], vs.Edit{Name: "changed.txt", Tags: []string{}}); e != nil {
		t.Fatal(e)
	}
	if _, e := SuggestTags(ctx, s, key, tagEmbedder{}, nil, thresholds); !errors.Is(e, ErrStale) {
		t.Fatal("stale index accepted", e)
	}
}

func TestNativeOrganizationJobDescriptionWithIdentityResume(t *testing.T) {
	root := os.Getenv("KLEFKI_MODEL_DIR")
	if root == "" || !semantic.Supported() {
		if os.Getenv("KLEFKI_REQUIRE_ONNX") == "1" {
			t.Fatal("required ONNX unavailable")
		}
		t.Skip("requires native model")
	}
	model, err := semantic.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer model.Close()
	account, _ := vc.RandomID()
	key, _ := vc.RandomKey()
	defer vc.Wipe(key[:])
	s, err := vs.Open(filepath.Join(t.TempDir(), "local.db"), account)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	fixtures := []struct {
		name, body string
		tags       []string
	}{
		{"resume.txt", "Alex Morgan. Software Engineer. Professional summary: builds backend services and APIs. Skills: Go, Python, SQL, Docker, Kubernetes. Experience: developed microservices, designed REST APIs, improved database performance and collaborated with engineering teams. Education: Bachelor of Science in Computer Science.", []string{"Identity"}},
		{"job description.txt", "Job description: Software Engineer. We are hiring a software engineer to build backend services and APIs. Responsibilities: develop microservices, design REST APIs, improve database performance and collaborate with engineering teams. Required qualifications: Bachelor of Science in Computer Science, experience with Go, Python, SQL, Docker and Kubernetes. Full-time employment with competitive salary and benefits.", []string{}},
		{"hospital vacancy.txt", "Job description: Data Analyst. About the employer: a hospital providing medical care, patient consultations, laboratory testing, prescriptions and clinical services. Role responsibilities: build reports and dashboards, analyse service performance, improve data quality and present insights to hospital management. Required qualifications: degree in computer science or statistics, SQL and Python skills, and experience with business intelligence tools. Full-time employment. Competitive salary, paid leave and employee benefits. Apply through the recruitment team.", []string{}},
		{"law firm vacancy.txt", "Job description: Data Analyst. About the employer: a law firm advising clients on legal rights, contracts, agreements, court proceedings and litigation. Role responsibilities: build reports and dashboards, analyse business performance, improve data quality and present insights to firm management. Required qualifications: degree in computer science or statistics, SQL and Python skills, and experience with business intelligence tools. Full-time employment. Competitive salary, paid leave and employee benefits. Apply through the recruitment team.", []string{}},
	}
	for _, f := range fixtures {
		id, _ := vc.RandomID()
		if _, err := s.Put(key, id.String(), "", vc.Metadata{Type: "file", Name: f.name, Tags: f.tags, Size: uint64(len(f.body))}, strings.NewReader(f.body)); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	if _, err := BuildSemantic(ctx, s, key, Extractor{}, model); err != nil {
		t.Fatal(err)
	}
	plan, err := SuggestTags(ctx, s, key, model, nil, semantic.DefaultThresholds)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]TagSuggestion{}
	for _, row := range plan.Suggestions {
		byName[row.Name] = row
	}
	for _, f := range fixtures[1:] {
		row, ok := byName[f.name]
		if !ok || row.Tag != "Work" || row.New {
			t.Errorf("%s matched %q instead of Work (basis %s, similarity %.3f)", f.name, row.Tag, row.Basis, row.Score)
		}
	}
}
