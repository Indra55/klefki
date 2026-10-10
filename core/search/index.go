// Package search builds a rebuildable, encrypted BM25 index using normal klefki files.
package search

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"sort"
	"strings"
	"unicode"

	vc "klefki/core/crypto"
	"klefki/core/semantic"
	vs "klefki/core/sync"
)

const IndexTag = "klefki:system:search-index"
const extractionTag = "klefki:system:search-image-cache-v1"
const maxIndex = 32 << 20
const maxDocument = 64 << 20

var ErrStale = errors.New("SEARCH_REBUILD_REQUIRED")
var ErrLimit = errors.New("SEARCH_LIMIT")
var ErrIndex = errors.New("SEARCH_INDEX_INVALID")

type document struct {
	Object   string         `json:"object"`
	Revision string         `json:"revision"`
	Terms    map[string]int `json:"terms"`
	Length   int            `json:"length"`
}
type index struct {
	Version   int        `json:"version"`
	Language  string     `json:"language"`
	Documents []document `json:"documents"`
}
type Report struct {
	Documents    int `json:"documents"`
	MetadataOnly int `json:"metadataOnly"`
}
type Hit struct {
	Object   string  `json:"object"`
	Revision string  `json:"revision"`
	Name     string  `json:"name"`
	Score    float64 `json:"score"`
}

func IsIndex(m vc.Metadata) bool {
	for _, tag := range m.Tags {
		if tag == IndexTag {
			return true
		}
	}
	return false
}
func frequencies(text string) map[string]int {
	out := map[string]int{}
	for _, term := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) && !unicode.IsMark(r) }) {
		if len(term) <= 128 {
			out[term]++
		}
	}
	return out
}
func documents(heads []vs.Head) []vs.Head {
	byObject := map[string][]vs.Head{}
	for _, h := range heads {
		byObject[h.Object] = append(byObject[h.Object], h)
	}
	var hidden func(vs.Head, map[string]bool) bool
	hidden = func(h vs.Head, seen map[string]bool) bool {
		if IsInternal(h.Metadata) {
			return true
		}
		for _, tag := range h.Metadata.Tags {
			if tag == "klefki:system:trash" {
				return true
			}
		}
		if seen[h.Object] {
			return true
		}
		if h.Metadata.Parent == nil {
			return false
		}
		seen[h.Object] = true
		for _, p := range byObject[*h.Metadata.Parent] {
			if hidden(p, seen) {
				return true
			}
		}
		delete(seen, h.Object)
		return false
	}
	out := []vs.Head{}
	for _, h := range heads {
		if h.Metadata.Type == "file" && !hidden(h, map[string]bool{}) {
			out = append(out, h)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Revision < out[j].Revision })
	return out
}
func sameDocuments(a, b []vs.Head) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Revision != b[i].Revision || a[i].Object != b[i].Object {
			return false
		}
	}
	return true
}

type boundedBuffer struct {
	bytes.Buffer
	max int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.max-b.Len() {
		return 0, ErrLimit
	}
	return b.Buffer.Write(p)
}
func Build(ctx context.Context, s *vs.Store, key vc.Key, extractor Extractor) (Report, error) {
	return build(ctx, s, key, extractor, nil)
}
func build(ctx context.Context, s *vs.Store, key vc.Key, extractor Extractor, model semantic.Embedder) (Report, error) {
	defer vc.Wipe(key[:])
	report := Report{}
	if e := ctx.Err(); e != nil {
		return report, e
	}
	heads, e := s.Heads(key)
	if e != nil {
		return report, e
	}
	docs := documents(heads)
	ix := index{Version: 1, Language: extractor.language(), Documents: []document{}}
	sx := SemanticIndex{Version: 1, Extraction: extractionTag, Documents: []SemanticDocument{}, Labels: [][]float32{}}
	defer sx.Clear()
	if model != nil {
		sx.Fingerprint = model.Fingerprint()
		for _, label := range semantic.Labels {
			v, e := semantic.Embed(ctx, model, label.Description)
			if e != nil {
				return report, e
			}
			sx.Labels = append(sx.Labels, v)
		}
	}
	budget := 0
	semanticBudget := 0
	for _, h := range docs {
		if e = ctx.Err(); e != nil {
			return report, e
		}
		var text string
		if !extractor.Supports(h.Metadata.Name) {
			report.MetadataOnly++
		} else {
			if h.Metadata.Size > maxDocument {
				return report, ErrLimit
			}
			buf := &boundedBuffer{max: maxDocument}
			e = s.ExportContext(ctx, key, h.Revision, buf)
			if e != nil {
				vc.Wipe(buf.Bytes())
				return report, e
			}
			text, e = extractor.Text(ctx, h.Metadata.Name, buf.Bytes())
			vc.Wipe(buf.Bytes())
			if e != nil {
				return report, e
			}
		}
		if model != nil {
			d, e := buildSemanticDocument(ctx, model, h, text)
			if e != nil {
				return report, e
			}
			semanticBudget += (len(d.Chunks)+2)*semantic.Dimension*16 + len(text)/4 + 4096
			if semanticBudget > maxIndex {
				return report, ErrLimit
			}
			sx.Documents = append(sx.Documents, d)
		}
		terms := frequencies(h.Metadata.Name + " " + strings.Join(h.Metadata.Tags, " ") + " " + text)
		length := 0
		for term, n := range terms {
			length += n
			budget += len(term) + 16
		}
		budget += 256
		if budget > maxIndex {
			return report, ErrLimit
		}
		ix.Documents = append(ix.Documents, document{h.Object, h.Revision, terms, length})
		report.Documents++
	}
	if model != nil {
		if e := validateSemantic(sx); e != nil {
			return report, e
		}
	}
	data, e := json.Marshal(ix)
	if e != nil {
		return report, ErrIndex
	}
	defer vc.Wipe(data)
	if len(data) > maxIndex {
		return report, ErrLimit
	}
	now, e := s.Heads(key)
	if e != nil {
		return report, e
	}
	if !sameDocuments(docs, documents(now)) {
		return report, ErrStale
	}
	object, base := "", ""
	for _, h := range now {
		if IsIndex(h.Metadata) {
			object, base = h.Object, h.Revision
			break
		}
	}
	if object == "" {
		id, e := vc.RandomID()
		if e != nil {
			return report, e
		}
		object = id.String()
	}
	_, e = s.PutContext(ctx, key, object, base, vc.Metadata{Type: "file", Name: "search-index.v1.json", Tags: []string{IndexTag, extractionTag}, Size: uint64(len(data))}, bytes.NewReader(data))
	if e == nil && model != nil {
		if ctx.Err() != nil {
			return report, ctx.Err()
		}
		now, e = s.Heads(key)
		if e != nil {
			return report, e
		}
		if !sameDocuments(docs, documents(now)) {
			return report, ErrStale
		}
		_, e = publishSidecar(ctx, s, key, now, SemanticTag, "semantic-index.v1.json", sx)
	}
	return report, e
}
func load(ctx context.Context, s *vs.Store, key vc.Key, heads, docs []vs.Head) (index, error) {
	defer vc.Wipe(key[:])
	for _, h := range heads {
		if e := ctx.Err(); e != nil {
			return index{}, e
		}
		if !IsIndex(h.Metadata) {
			continue
		}
		current := false
		for _, tag := range h.Metadata.Tags {
			if tag == extractionTag {
				current = true
			}
		}
		if !current {
			continue
		}
		if h.Metadata.Type != "file" || h.Metadata.Size > maxIndex {
			return index{}, ErrIndex
		}
		buf := &boundedBuffer{max: maxIndex}
		e := s.ExportContext(ctx, key, h.Revision, buf)
		if e != nil {
			vc.Wipe(buf.Bytes())
			return index{}, e
		}
		var ix index
		d := json.NewDecoder(bytes.NewReader(buf.Bytes()))
		d.DisallowUnknownFields()
		e = d.Decode(&ix)
		var extra any
		if e == nil && d.Decode(&extra) != io.EOF {
			e = ErrIndex
		}
		vc.Wipe(buf.Bytes())
		if e != nil || ix.Version != 1 {
			return index{}, ErrIndex
		}
		if len(ix.Documents) != len(docs) {
			continue
		}
		match := true
		for i, doc := range ix.Documents {
			if doc.Revision != docs[i].Revision || doc.Object != docs[i].Object {
				match = false
				break
			}
			length := 0
			for term, n := range doc.Terms {
				if n <= 0 || n > maxDocument || len(term) > 128 || frequencies(term)[term] != 1 {
					return index{}, ErrIndex
				}
				length += n
				if length > maxDocument {
					return index{}, ErrIndex
				}
			}
			if length != doc.Length {
				return index{}, ErrIndex
			}
		}
		if match {
			return ix, nil
		}
	}
	return index{}, ErrStale
}
func Query(ctx context.Context, s *vs.Store, key vc.Key, query string) ([]Hit, error) {
	defer vc.Wipe(key[:])
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if len(query) > 4096 {
		return nil, ErrLimit
	}
	heads, e := s.Heads(key)
	if e != nil {
		return nil, e
	}
	docs := documents(heads)
	ix, e := load(ctx, s, key, heads, docs)
	if e != nil {
		return nil, e
	}
	terms := frequencies(query)
	if len(terms) > 64 {
		return nil, ErrLimit
	}
	queryTerms := relevantQueryTerms(terms)
	hits := []Hit{}
	if len(docs) == 0 || len(queryTerms) == 0 {
		return hits, nil
	}
	average := 0.0
	for _, doc := range ix.Documents {
		average += float64(doc.Length)
	}
	average /= float64(len(docs))
	if average == 0 {
		return hits, nil
	}
	df := map[string]int{}
	for _, doc := range ix.Documents {
		for _, term := range queryTerms {
			if doc.Terms[term] > 0 {
				df[term]++
			}
		}
	}
	for i, doc := range ix.Documents {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		score := 0.0
		for _, term := range queryTerms {
			tf := float64(doc.Terms[term])
			if tf == 0 {
				continue
			}
			idf := math.Log1p((float64(len(docs)-df[term]) + 0.5) / (float64(df[term]) + 0.5))
			score += idf * tf * 2.2 / (tf + 1.2*(0.25+0.75*float64(doc.Length)/average))
		}
		if score > 0 && keywordEvidence(queryTerms, doc.Terms) {
			hits = append(hits, Hit{doc.Object, doc.Revision, docs[i].Metadata.Name, score})
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score == hits[j].Score {
			return hits[i].Revision < hits[j].Revision
		}
		return hits[i].Score > hits[j].Score
	})
	now, e := s.Heads(key)
	if e != nil {
		return nil, e
	}
	if !sameDocuments(docs, documents(now)) {
		return nil, ErrStale
	}
	return hits, nil
}
