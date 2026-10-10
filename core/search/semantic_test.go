package search

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"

	vc "klefki/core/crypto"
	"klefki/core/semantic"
	vs "klefki/core/sync"
)

type testEmbedder struct{}

type legacyEmbedder struct{ testEmbedder }

func (legacyEmbedder) Fingerprint() string { return "legacy-minilm" }

func (testEmbedder) Fingerprint() string { return semantic.Fingerprint() }
func (testEmbedder) Tokens(s string) ([]int64, error) {
	t := []int64{}
	for _, r := range s {
		t = append(t, int64(r))
	}
	return t, nil
}
func (testEmbedder) EmbedTokens(ctx context.Context, t []int64) ([]float32, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	v := make([]float32, semantic.Dimension)
	v[0] = 1
	for _, x := range t {
		v[1+int(x)%383]++
	}
	semantic.Normalize(v)
	return v, nil
}
func (testEmbedder) Close() {}

func TestSemanticSidecarsCorrectionsAndStaleness(t *testing.T) {
	a, key, _, e := vc.NewAccount([]byte("public M8 fixture password"))
	if e != nil {
		t.Fatal(e)
	}
	defer vc.Wipe(key[:])
	s, e := vs.Open(filepath.Join(t.TempDir(), "local.db"), a.ID)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	id, _ := vc.RandomID()
	text := "Synthetic electricity bill issued 2026-04-12. Amount INR 1,234.50."
	rev, e := s.Put(key, id.String(), "", vc.Metadata{Type: "file", Name: "bill.txt", Tags: []string{}, Size: uint64(len(text))}, bytes.NewBufferString(text))
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	model := testEmbedder{}
	if _, e = BuildSemantic(ctx, s, key, Extractor{}, model); e != nil {
		t.Fatal(e)
	}
	h, e := QueryMode(ctx, s, key, model, "electricity", "hybrid")
	if e != nil || len(h) != 1 || h[0].Revision != rev {
		t.Fatal(h, e)
	}
	analysis, e := Analyze(ctx, s, key, rev, semantic.DefaultThresholds)
	if e != nil || len(analysis.Fields) != 2 {
		t.Fatal(analysis, e)
	}
	if _, e = SaveCorrection(ctx, s, key, rev, "Money", analysis.Fields, nil); e != nil {
		t.Fatal(e)
	}
	if _, e = BuildSemantic(ctx, s, key, Extractor{}, legacyEmbedder{}); e != nil {
		t.Fatal(e)
	}
	if _, e = Analyze(ctx, s, key, rev, semantic.DefaultThresholds); !errors.Is(e, ErrStale) {
		t.Fatal("analysis accepted the previous model index", e)
	}
	if _, e = BuildSemantic(ctx, s, key, Extractor{}, model); e != nil {
		t.Fatal(e)
	}
	newrev, e := s.Edit(key, rev, vs.Edit{Name: "renamed.txt", Tags: []string{}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = QueryMode(ctx, s, key, model, "bill", "semantic"); !errors.Is(e, ErrStale) {
		t.Fatal("stale", e)
	}
	if _, e = BuildSemantic(ctx, s, key, Extractor{}, model); e != nil {
		t.Fatal(e)
	}
	analysis, e = Analyze(ctx, s, key, newrev, semantic.DefaultThresholds)
	if e != nil || analysis.Confirmed != "Money" {
		t.Fatal("correction lost on metadata edit", e)
	}
	heads, e := s.Heads(key)
	if e != nil || len(documents(heads)) != 1 {
		t.Fatal("sidecars indexed", e)
	}
	cancel, c := context.WithCancel(ctx)
	c()
	if _, e = BuildSemantic(cancel, s, key, Extractor{}, model); !errors.Is(e, context.Canceled) {
		t.Fatal("cancellation", e)
	}
	if _, e = Query(ctx, s, key, "electricity"); e != nil {
		t.Fatal("BM25 affected", e)
	}
}

func TestFusionAndVectorValidation(t *testing.T) {
	a := []Hit{{Revision: "b", Score: 5}, {Revision: "a", Score: 4}}
	b := []Hit{{Revision: "a", Score: .9}, {Revision: "b", Score: .8}}
	h := Fuse(a, b)
	if len(h) != 2 || h[0].Revision != "a" {
		t.Fatal(h)
	}
	x := SemanticIndex{Version: 1, Fingerprint: "x", Labels: [][]float32{}}
	if validateSemantic(x) == nil {
		t.Fatal("invalid index accepted")
	}
}

func TestCorrectionConflictsAndContentReplacement(t *testing.T) {
	a, key, _, e := vc.NewAccount([]byte("public M8 correction fixture"))
	if e != nil {
		t.Fatal(e)
	}
	defer vc.Wipe(key[:])
	s, e := vs.Open(filepath.Join(t.TempDir(), "local.db"), a.ID)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ctx := context.Background()
	model := testEmbedder{}
	id, _ := vc.RandomID()
	text := "PUBLIC SYNTHETIC power consumption document"
	rev, e := s.Put(key, id.String(), "", vc.Metadata{Type: "file", Name: "bill.txt", Tags: []string{}, Size: uint64(len(text))}, bytes.NewBufferString(text))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = BuildSemantic(ctx, s, key, Extractor{}, model); e != nil {
		t.Fatal(e)
	}
	head, e := s.Revision(key, rev)
	if e != nil {
		t.Fatal(e)
	}
	for _, category := range []string{"Money", "Work"} {
		_, e = publishSidecar(ctx, s, key, nil, CorrectionTag, "analysis-correction.v1.json", Correction{Version: 1, Object: id.String(), FileID: head.Metadata.FileID, Category: category, Fields: []semantic.Field{}, Supersedes: []string{}})
		if e != nil {
			t.Fatal(e)
		}
	}
	analysis, e := Analyze(ctx, s, key, rev, semantic.DefaultThresholds)
	if e != nil || len(analysis.Conflicts) != 2 {
		t.Fatal("conflicts missing", e)
	}
	if _, e = SaveCorrection(ctx, s, key, rev, "Money", nil, nil); !errors.Is(e, ErrCorrectionConflict) {
		t.Fatal("silent correction winner", e)
	}
	resolve := []string{}
	for _, r := range analysis.Conflicts {
		resolve = append(resolve, r.Revision)
	}
	if _, e = SaveCorrection(ctx, s, key, rev, "Money", nil, resolve); e != nil {
		t.Fatal(e)
	}
	analysis, e = Analyze(ctx, s, key, rev, semantic.DefaultThresholds)
	if e != nil || analysis.Confirmed != "Money" || len(analysis.Conflicts) != 0 {
		t.Fatal("resolution failed", e)
	}
	newText := "PUBLIC SYNTHETIC university academic degree"
	newrev, e := s.Put(key, id.String(), rev, vc.Metadata{Type: "file", Name: "degree.txt", Tags: []string{}, Size: uint64(len(newText))}, bytes.NewBufferString(newText))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = BuildSemantic(ctx, s, key, Extractor{}, model); e != nil {
		t.Fatal(e)
	}
	analysis, e = Analyze(ctx, s, key, newrev, semantic.DefaultThresholds)
	if e != nil || analysis.Confirmed != "" {
		t.Fatal("correction crossed content replacement", e)
	}
}

func TestKNNUsesConfirmedOtherDocumentsOnly(t *testing.T) {
	a, key, _, e := vc.NewAccount([]byte("public M8 neighbors fixture"))
	if e != nil {
		t.Fatal(e)
	}
	defer vc.Wipe(key[:])
	s, e := vs.Open(filepath.Join(t.TempDir(), "local.db"), a.ID)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ctx := context.Background()
	model := testEmbedder{}
	revisions := []string{}
	for i := 0; i < 4; i++ {
		id, _ := vc.RandomID()
		text := "PUBLIC SYNTHETIC electricity power consumption receipt"
		rev, e := s.Put(key, id.String(), "", vc.Metadata{Type: "file", Name: "bill.txt", Tags: []string{}, Size: uint64(len(text))}, bytes.NewBufferString(text))
		if e != nil {
			t.Fatal(e)
		}
		revisions = append(revisions, rev)
	}
	if _, e = BuildSemantic(ctx, s, key, Extractor{}, model); e != nil {
		t.Fatal(e)
	}
	for _, rev := range revisions[:3] {
		if _, e = SaveCorrection(ctx, s, key, rev, "Money", nil, nil); e != nil {
			t.Fatal(e)
		}
	}
	a1, e := Analyze(ctx, s, key, revisions[3], semantic.DefaultThresholds)
	if e != nil || a1.Prediction.Method != "knn" || a1.Prediction.Category != "Money" {
		t.Fatal(a1, e)
	}
	a2, e := Analyze(ctx, s, key, revisions[0], semantic.DefaultThresholds)
	if e != nil || a2.Prediction.Method == "knn" {
		t.Fatal("self used as neighbor", e)
	}
}
