package search

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"

	vc "vault/core/crypto"
	"vault/core/semantic"
	vs "vault/core/sync"
)

const SemanticTag = "vault:system:semantic-index"
const CorrectionTag = "vault:system:analysis-correction"

var ErrCorrectionConflict = errors.New("SEMANTIC_CORRECTION_CONFLICT")

type SemanticDocument struct {
	Object   string           `json:"object"`
	Revision string           `json:"revision"`
	FileID   string           `json:"fileID"`
	Name     []float32        `json:"name"`
	Chunks   [][]float32      `json:"chunks"`
	Vector   []float32        `json:"vector"`
	Fields   []semantic.Field `json:"fields"`
}
type SemanticIndex struct {
	Version     int                `json:"version"`
	Fingerprint string             `json:"fingerprint"`
	Extraction  string             `json:"extraction"`
	Labels      [][]float32        `json:"labels"`
	Documents   []SemanticDocument `json:"documents"`
}

func (x *SemanticIndex) Clear() {
	for i := range x.Documents {
		d := &x.Documents[i]
		clear(d.Name)
		clear(d.Vector)
		for _, v := range d.Chunks {
			clear(v)
		}
	}
	for _, v := range x.Labels {
		clear(v)
	}
	x.Documents = nil
	x.Labels = nil
}
func hasTag(m vc.Metadata, tag string) bool {
	for _, t := range m.Tags {
		if t == tag {
			return true
		}
	}
	return false
}
func IsInternal(m vc.Metadata) bool {
	return IsIndex(m) || hasTag(m, SemanticTag) || hasTag(m, CorrectionTag)
}

func buildSemanticDocument(ctx context.Context, model semantic.Embedder, h vs.Head, text string) (SemanticDocument, error) {
	d := SemanticDocument{Object: h.Object, Revision: h.Revision, FileID: h.Metadata.FileID, Fields: semantic.Fields(text)}
	var e error
	d.Name, e = semantic.Embed(ctx, model, h.Metadata.Name)
	if e != nil {
		return d, e
	}
	d.Chunks, e = semantic.Chunks(ctx, model, text)
	if e != nil {
		clear(d.Name)
		return d, e
	}
	d.Vector = semantic.Mean(d.Chunks)
	return d, nil
}
func BuildSemantic(ctx context.Context, s *vs.Store, key vc.Key, x Extractor, model semantic.Embedder) (Report, error) {
	if model == nil {
		return Report{}, semantic.ErrUnavailable
	}
	return build(ctx, s, key, x, model)
}
func publishSidecar(ctx context.Context, s *vs.Store, key vc.Key, heads []vs.Head, tag, name string, payload any) (string, error) {
	data, e := json.Marshal(payload)
	if e != nil {
		return "", ErrIndex
	}
	defer vc.Wipe(data)
	if len(data) > maxIndex {
		return "", ErrLimit
	}
	object, base := "", ""
	if tag != CorrectionTag {
		for _, h := range heads {
			if hasTag(h.Metadata, tag) {
				object, base = h.Object, h.Revision
				break
			}
		}
	}
	if object == "" {
		id, e := vc.RandomID()
		if e != nil {
			return "", e
		}
		object = id.String()
	}
	return s.PutContext(ctx, key, object, base, vc.Metadata{Type: "file", Name: name, Tags: []string{tag}, Size: uint64(len(data))}, bytes.NewReader(data))
}
func readSidecar(ctx context.Context, s *vs.Store, key vc.Key, h vs.Head, out any) error {
	if h.Metadata.Type != "file" || h.Metadata.Size > maxIndex {
		return ErrIndex
	}
	b := &boundedBuffer{max: maxIndex}
	defer func() { vc.Wipe(b.Bytes()) }()
	if e := s.ExportContext(ctx, key, h.Revision, b); e != nil {
		return e
	}
	d := json.NewDecoder(bytes.NewReader(b.Bytes()))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil {
		return ErrIndex
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return ErrIndex
	}
	return nil
}
func validateSemantic(x SemanticIndex) error {
	if x.Version != 1 || len(x.Fingerprint) == 0 || len(x.Fingerprint) > 128 || x.Extraction != extractionTag || len(x.Labels) != len(semantic.Labels) || x.Documents == nil {
		return ErrIndex
	}
	for _, v := range x.Labels {
		if !semantic.ValidVector(v) {
			return ErrIndex
		}
	}
	seen := map[string]bool{}
	for _, d := range x.Documents {
		for _, id := range []string{d.Object, d.Revision, d.FileID} {
			if _, e := vc.ParseID(id); e != nil {
				return ErrIndex
			}
		}
		if seen[d.Revision] || !semantic.ValidVector(d.Name) || !semantic.ValidFields(d.Fields) || d.Chunks == nil {
			return ErrIndex
		}
		seen[d.Revision] = true
		for _, v := range d.Chunks {
			if !semantic.ValidVector(v) {
				return ErrIndex
			}
		}
		if len(d.Chunks) > 0 && !semantic.ValidVector(d.Vector) || len(d.Chunks) == 0 && len(d.Vector) != 0 {
			return ErrIndex
		}
	}
	return nil
}
func loadSemantic(ctx context.Context, s *vs.Store, key vc.Key, heads, docs []vs.Head, fingerprint string) (SemanticIndex, error) {
	for _, h := range heads {
		if !hasTag(h.Metadata, SemanticTag) {
			continue
		}
		var ix SemanticIndex
		if e := readSidecar(ctx, s, key, h, &ix); e != nil {
			return ix, e
		}
		if e := validateSemantic(ix); e != nil {
			ix.Clear()
			return SemanticIndex{}, e
		}
		if (fingerprint != "" && ix.Fingerprint != fingerprint) || len(ix.Documents) != len(docs) {
			ix.Clear()
			continue
		}
		match := true
		for i, d := range ix.Documents {
			if d.Object != docs[i].Object || d.Revision != docs[i].Revision || d.FileID != docs[i].Metadata.FileID {
				match = false
				break
			}
		}
		if match {
			return ix, nil
		}
		ix.Clear()
	}
	return SemanticIndex{}, ErrStale
}
func rank(hits []Hit) []Hit {
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score == hits[j].Score {
			return hits[i].Revision < hits[j].Revision
		}
		return hits[i].Score > hits[j].Score
	})
	return hits
}
func Fuse(keyword, vector []Hit) []Hit {
	m := map[string]Hit{}
	for _, list := range [][]Hit{keyword, vector} {
		for i, h := range list[:min(50, len(list))] {
			old := m[h.Revision]
			if old.Revision == "" {
				old = h
				old.Score = 0
			}
			old.Score += 1 / float64(61+i)
			m[h.Revision] = old
		}
	}
	out := make([]Hit, 0, len(m))
	for _, h := range m {
		out = append(out, h)
	}
	rank(out)
	return out[:min(20, len(out))]
}
func QueryMode(ctx context.Context, s *vs.Store, key vc.Key, model semantic.Embedder, query, mode string) ([]Hit, error) {
	defer vc.Wipe(key[:])
	if mode == "keyword" {
		return Query(ctx, s, key, query)
	}
	if mode != "semantic" && mode != "hybrid" {
		return nil, ErrIndex
	}
	if model == nil {
		return nil, semantic.ErrUnavailable
	}
	if len(query) > 4096 {
		return nil, ErrLimit
	}
	if strings.TrimSpace(query) == "" {
		return []Hit{}, nil
	}
	head, e := s.Heads(key)
	if e != nil {
		return nil, e
	}
	docs := documents(head)
	ix, e := loadSemantic(ctx, s, key, head, docs, model.Fingerprint())
	if e != nil {
		return nil, e
	}
	defer ix.Clear()
	config := semantic.ModelManifest()
	t, e := model.Tokens(config.QueryInstruction + query)
	if e != nil {
		return nil, e
	}
	defer clear(t)
	v, e := model.EmbedTokens(ctx, t)
	if e != nil {
		return nil, e
	}
	defer clear(v)
	if !semantic.ValidVector(v) {
		return nil, semantic.ErrModel
	}
	hits := []Hit{}
	shortQuery := len(relevantQueryTerms(frequencies(query))) <= 3
	for i, d := range ix.Documents {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		score := -1.0
		if shortQuery {
			score = semantic.Cosine(v, d.Name)
		}
		for _, c := range d.Chunks {
			score = max(score, semantic.Cosine(v, c))
		}
		if score >= config.SearchCutoff {
			hits = append(hits, Hit{d.Object, d.Revision, docs[i].Metadata.Name, score})
		}
	}
	hits = relevantSemanticHits(hits)
	if mode == "hybrid" {
		kw, e := Query(ctx, s, key, query)
		if e != nil {
			return nil, e
		}
		hits = Fuse(kw, hits)
	} else {
		hits = hits[:min(20, len(hits))]
	}
	now, e := s.Heads(key)
	if e != nil {
		return nil, e
	}
	if !sameDocuments(docs, documents(now)) {
		return nil, ErrStale
	}
	return hits, nil
}

type Correction struct {
	Version    int              `json:"version"`
	Object     string           `json:"object"`
	FileID     string           `json:"fileID"`
	Category   string           `json:"category"`
	Fields     []semantic.Field `json:"fields"`
	Supersedes []string         `json:"supersedes"`
}
type CorrectionRecord struct {
	Revision   string     `json:"revision"`
	Correction Correction `json:"correction"`
}

func Corrections(ctx context.Context, s *vs.Store, key vc.Key, heads []vs.Head) ([]CorrectionRecord, error) {
	out := []CorrectionRecord{}
	superseded := map[string]bool{}
	for _, h := range heads {
		if !hasTag(h.Metadata, CorrectionTag) {
			continue
		}
		var c Correction
		if e := readSidecar(ctx, s, key, h, &c); e != nil {
			return nil, e
		}
		if c.Version != 1 || !semantic.ValidCategory(c.Category) || !semantic.ValidFields(c.Fields) || len(c.Supersedes) > 64 {
			return nil, ErrIndex
		}
		for _, id := range append([]string{c.Object, c.FileID}, c.Supersedes...) {
			if _, e := vc.ParseID(id); e != nil {
				return nil, ErrIndex
			}
		}
		out = append(out, CorrectionRecord{h.Revision, c})
	}
	byID := map[string]CorrectionRecord{}
	for _, r := range out {
		byID[r.Revision] = r
	}
	for _, r := range out {
		for _, id := range r.Correction.Supersedes {
			other, ok := byID[id]
			if !ok || other.Correction.Object != r.Correction.Object || other.Correction.FileID != r.Correction.FileID || id == r.Revision {
				return nil, ErrIndex
			}
			superseded[id] = true
		}
	}
	active := []CorrectionRecord{}
	for _, r := range out {
		if !superseded[r.Revision] {
			active = append(active, r)
		}
	}
	sort.Slice(active, func(i, j int) bool { return active[i].Revision < active[j].Revision })
	return active, nil
}
func SaveCorrection(ctx context.Context, s *vs.Store, key vc.Key, revision, category string, fields []semantic.Field, resolve []string) (string, error) {
	defer vc.Wipe(key[:])
	if !semantic.ValidCategory(category) || !semantic.ValidFields(fields) {
		return "", ErrIndex
	}
	heads, e := s.Heads(key)
	if e != nil {
		return "", e
	}
	var source *vs.Head
	for _, h := range documents(heads) {
		if h.Revision == revision {
			h := h
			source = &h
			break
		}
	}
	if source == nil {
		return "", ErrStale
	}
	all, e := Corrections(ctx, s, key, heads)
	if e != nil {
		return "", e
	}
	existing := []string{}
	for _, r := range all {
		if r.Correction.Object == source.Object && r.Correction.FileID == source.Metadata.FileID {
			existing = append(existing, r.Revision)
		}
	}
	if len(existing) > 1 {
		sort.Strings(resolve)
		sort.Strings(existing)
		if len(resolve) != len(existing) {
			return "", ErrCorrectionConflict
		}
		for i := range existing {
			if resolve[i] != existing[i] {
				return "", ErrCorrectionConflict
			}
		}
	}
	if fields == nil {
		fields = []semantic.Field{}
	}
	c := Correction{1, source.Object, source.Metadata.FileID, category, fields, existing}
	return publishSidecar(ctx, s, key, heads, CorrectionTag, "analysis-correction.v1.json", c)
}

type Analysis struct {
	Prediction semantic.Prediction `json:"prediction"`
	Confirmed  string              `json:"confirmed"`
	Fields     []semantic.Field    `json:"fields"`
	Conflicts  []CorrectionRecord  `json:"conflicts"`
}

func Analyze(ctx context.Context, s *vs.Store, key vc.Key, revision string, t semantic.Thresholds) (Analysis, error) {
	defer vc.Wipe(key[:])
	out := Analysis{Fields: []semantic.Field{}, Conflicts: []CorrectionRecord{}}
	if !t.Valid() {
		return out, ErrIndex
	}
	heads, e := s.Heads(key)
	if e != nil {
		return out, e
	}
	docs := documents(heads)
	ix, e := loadSemantic(ctx, s, key, heads, docs, semantic.Fingerprint())
	if e != nil {
		return out, e
	}
	defer ix.Clear()
	var doc *SemanticDocument
	for i := range ix.Documents {
		if ix.Documents[i].Revision == revision {
			doc = &ix.Documents[i]
			break
		}
	}
	if doc == nil {
		return out, ErrStale
	}
	out.Fields = doc.Fields
	out.Prediction = semantic.Classify(doc.Vector, ix.Labels, t)
	cs, e := Corrections(ctx, s, key, heads)
	if e != nil {
		return out, e
	}
	for _, r := range cs {
		if r.Correction.Object == doc.Object && r.Correction.FileID == doc.FileID {
			out.Conflicts = append(out.Conflicts, r)
		}
	}
	if len(out.Conflicts) == 1 {
		out.Confirmed = out.Conflicts[0].Correction.Category
		out.Fields = out.Conflicts[0].Correction.Fields
		out.Conflicts = []CorrectionRecord{}
	} else if len(out.Conflicts) > 1 {
		return out, nil
	}
	type neighbor struct {
		category string
		score    float64
		id       string
	}
	neighbors := []neighbor{}
	for _, d := range ix.Documents {
		if d.Object == doc.Object || !semantic.ValidVector(d.Vector) {
			continue
		}
		matches := []CorrectionRecord{}
		for _, r := range cs {
			if r.Correction.Object == d.Object && r.Correction.FileID == d.FileID && r.Correction.Category != "" {
				matches = append(matches, r)
			}
		}
		if len(matches) != 1 {
			continue
		}
		score := semantic.Cosine(doc.Vector, d.Vector)
		if score >= .5 {
			neighbors = append(neighbors, neighbor{matches[0].Correction.Category, score, d.Revision})
		}
	}
	sort.Slice(neighbors, func(i, j int) bool {
		if neighbors[i].score == neighbors[j].score {
			return neighbors[i].id < neighbors[j].id
		}
		return neighbors[i].score > neighbors[j].score
	})
	neighbors = neighbors[:min(5, len(neighbors))]
	if len(neighbors) >= 3 {
		weights := map[string]float64{}
		total := 0.0
		for _, n := range neighbors {
			weights[n.category] += n.score
			total += n.score
		}
		for _, l := range semantic.Labels {
			if weights[l.Name]/total >= .6 {
				out.Prediction = semantic.Prediction{Category: l.Name, Score: weights[l.Name] / total, Method: "knn"}
				break
			}
		}
	}
	return out, nil
}
