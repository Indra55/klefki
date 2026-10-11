package search

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	vc "klefki/core/crypto"
	"klefki/core/semantic"
	vs "klefki/core/sync"
)

type TagSuggestion struct {
	Revision string   `json:"revision"`
	Name     string   `json:"name"`
	Tags     []string `json:"tags"`
	Tag      string   `json:"tag"`
	New      bool     `json:"new"`
	Basis    string   `json:"basis"`
	Score    float64  `json:"score"`
}

type Organization struct {
	Suggestions []TagSuggestion `json:"suggestions"`
	Tags        []string        `json:"tags"`
	Skipped     int             `json:"skipped"`
}

type tagCandidate struct {
	name   string
	vector []float32
	detail []float32
	sum    []float32
	owners map[string][]float32
}

func (c tagCandidate) similarity(vector []float32) float64 {
	score := semantic.Cosine(vector, c.vector)
	if c.detail != nil {
		score = max(score, semantic.Cosine(vector, c.detail))
	}
	return score
}

func winningTag(scores []float64, thresholds semantic.Thresholds) (int, float64, bool) {
	best, second, winner := -2.0, -2.0, -1
	for i, score := range scores {
		if score > best {
			second, best, winner = best, score, i
		} else if score > second {
			second = score
		}
	}
	return winner, best, winner >= 0 && best >= thresholds.Cutoff && best-second >= thresholds.Margin
}

func categoryTagScores(d SemanticDocument, candidates []tagCandidate, thresholds semantic.Thresholds) []float64 {
	scores, strongest := make([]float64, len(candidates)), make([]float64, len(candidates))
	for i, c := range candidates {
		scores[i], strongest[i] = c.similarity(d.Vector), -2
	}
	category, conflicting := -1, false
	for _, chunk := range d.Chunks {
		chunkScores := make([]float64, len(candidates))
		for i, c := range candidates {
			chunkScores[i] = c.similarity(chunk)
			strongest[i] = max(strongest[i], chunkScores[i])
		}
		if winner, _, confident := winningTag(chunkScores, thresholds); confident {
			if category >= 0 && category != winner {
				conflicting = true
			}
			category = winner
		}
	}
	if conflicting {
		for i := range scores {
			scores[i] = -2
		}
		return scores
	}
	if category >= 0 {
		return strongest
	}
	return scores
}

// SuggestTags reuses the encrypted index; proposed tags never become examples.
func SuggestTags(ctx context.Context, s *vs.Store, key vc.Key, model semantic.Embedder, custom []string, thresholds semantic.Thresholds) (Organization, error) {
	defer vc.Wipe(key[:])
	out := Organization{Suggestions: []TagSuggestion{}, Tags: []string{}}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if model == nil {
		return out, semantic.ErrUnavailable
	}
	if !thresholds.Valid() || len(custom) > 64 {
		return out, vc.ErrInvalid
	}
	for _, tag := range custom {
		if !vs.ValidUserTag(tag) {
			return out, vc.ErrInvalid
		}
	}
	heads, err := s.Heads(key)
	if err != nil {
		return out, err
	}
	docs := documents(heads)
	ix, err := loadSemantic(ctx, s, key, heads, docs, model.Fingerprint())
	if err != nil {
		return out, err
	}
	defer ix.Clear()
	words, err := load(ctx, s, key, heads, docs)
	if err != nil {
		return out, err
	}
	corrections, err := Corrections(ctx, s, key, heads)
	if err != nil {
		return out, err
	}
	candidates := []tagCandidate{}
	byName := map[string]int{}
	add := func(name string, vector []float32) {
		fold := strings.ToLower(name)
		if _, ok := byName[fold]; ok || !vs.ValidUserTag(name) {
			return
		}
		byName[fold] = len(candidates)
		candidates = append(candidates, tagCandidate{name: name, vector: vector, sum: make([]float32, semantic.Dimension), owners: map[string][]float32{}})
	}
	for i, label := range semantic.Labels {
		add(label.Name, append([]float32(nil), ix.Labels[i]...))
	}
	defer func() {
		for _, c := range candidates {
			clear(c.vector)
			clear(c.detail)
			clear(c.sum)
		}
	}()
	work := &candidates[byName["work"]]
	work.detail, err = semantic.Embed(ctx, model, "Job descriptions and job postings outlining a role's responsibilities, required qualifications, professional skills, salary and employment benefits.")
	if err != nil {
		return out, err
	}
	if !semantic.ValidVector(work.detail) {
		return out, semantic.ErrModel
	}
	for i, h := range docs {
		if h.Conflict {
			continue
		}
		for _, tag := range h.Metadata.Tags {
			add(tag, nil)
			if j, ok := byName[strings.ToLower(tag)]; ok && j >= len(semantic.Labels) && semantic.ValidVector(ix.Documents[i].Vector) {
				c := &candidates[j]
				if _, exists := c.owners[h.Object]; !exists {
					c.owners[h.Object] = ix.Documents[i].Vector
					for k, value := range ix.Documents[i].Vector {
						c.sum[k] += value
					}
				}
			}
		}
	}
	for _, tag := range custom {
		add(tag, nil)
	}
	if len(candidates) > 256 {
		return out, ErrLimit
	}
	for i := range candidates {
		c := &candidates[i]
		if c.vector == nil {
			c.vector, err = semantic.Embed(ctx, model, "Documents about "+c.name+".")
			if err != nil {
				return out, err
			}
			if !semantic.ValidVector(c.vector) {
				return out, semantic.ErrModel
			}
		}
		out.Tags = append(out.Tags, c.name)
	}
	var unmatched []int
	for i, h := range docs {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		d := ix.Documents[i]
		if h.Conflict || len(h.Metadata.Tags) >= 64 || !semantic.ValidVector(d.Vector) {
			out.Skipped++
			continue
		}
		confirmed := []CorrectionRecord{}
		for _, c := range corrections {
			if c.Correction.Object == d.Object && c.Correction.FileID == d.FileID {
				confirmed = append(confirmed, c)
			}
		}
		if len(confirmed) > 1 {
			out.Skipped++
			continue
		}
		if len(confirmed) == 1 && confirmed[0].Correction.Category != "" {
			appendSuggestion(&out, h, confirmed[0].Correction.Category, false, "confirmed category", 0)
			continue
		}
		categoryScores := categoryTagScores(d, candidates[:len(semantic.Labels)], thresholds)
		scores := make([]float64, len(candidates))
		for j, c := range candidates {
			score := c.similarity(d.Vector)
			if j < len(categoryScores) {
				score = categoryScores[j]
			}
			if len(c.owners) > 0 {
				mean := append([]float32(nil), c.sum...)
				if own, exists := c.owners[d.Object]; exists {
					for k, value := range own {
						mean[k] -= value
					}
				}
				if semantic.Normalize(mean) {
					score = max(score, semantic.Cosine(d.Vector, mean))
				}
				clear(mean)
			}
			scores[j] = score
		}
		if winner, best, confident := winningTag(scores, thresholds); confident {
			appendSuggestion(&out, h, candidates[winner].name, false, "matching tag", best)
		} else if len(h.Metadata.Tags) > 0 {
			out.Skipped++
		} else {
			unmatched = append(unmatched, i)
		}
	}
	// Every member must match the seed and centroid, avoiding chains of weak matches.
	groups := [][]int{}
	centroids := [][]float32{}
	defer func() {
		for _, v := range centroids {
			clear(v)
		}
	}()
	for _, i := range unmatched {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		best, winner := .72, -1
		for j, group := range groups {
			score := min(semantic.Cosine(ix.Documents[i].Vector, centroids[j]), semantic.Cosine(ix.Documents[i].Vector, ix.Documents[group[0]].Vector))
			if score >= best {
				best, winner = score, j
			}
		}
		if winner < 0 {
			groups = append(groups, []int{i})
			centroids = append(centroids, append([]float32(nil), ix.Documents[i].Vector...))
		} else {
			groups[winner] = append(groups[winner], i)
			vectors := [][]float32{}
			for _, member := range groups[winner] {
				vectors = append(vectors, ix.Documents[member].Vector)
			}
			clear(centroids[winner])
			centroids[winner] = semantic.Mean(vectors)
		}
	}
	frequency := map[string]int{}
	for _, d := range words.Documents {
		for term := range d.Terms {
			frequency[term]++
		}
	}
	for j, group := range groups {
		name := groupTag(group, docs, words, frequency)
		base := name
		for suffix := 2; ; suffix++ {
			if _, exists := byName[strings.ToLower(name)]; !exists {
				break
			}
			name = fmt.Sprintf("%s %d", base, suffix)
		}
		byName[strings.ToLower(name)] = len(byName)
		out.Tags = append(out.Tags, name)
		for _, i := range group {
			appendSuggestion(&out, docs[i], name, true, "new content group", semantic.Cosine(ix.Documents[i].Vector, centroids[j]))
		}
	}
	sort.Strings(out.Tags)
	now, err := s.Heads(key)
	if err != nil {
		return out, err
	}
	if !sameDocuments(docs, documents(now)) {
		return out, ErrStale
	}
	return out, nil
}

func appendSuggestion(out *Organization, h vs.Head, tag string, fresh bool, basis string, score float64) {
	for _, t := range h.Metadata.Tags {
		if strings.EqualFold(t, tag) {
			out.Skipped++
			return
		}
	}
	out.Suggestions = append(out.Suggestions, TagSuggestion{h.Revision, h.Metadata.Name, h.Metadata.Tags, tag, fresh, basis, score})
}

func groupTag(group []int, docs []vs.Head, ix index, frequency map[string]int) string {
	scores := map[string]float64{}
	for _, i := range group {
		name := frequencies(strings.TrimSuffix(docs[i].Metadata.Name, filepath.Ext(docs[i].Metadata.Name)))
		for term, count := range ix.Documents[i].Terms {
			if len(term) < 3 || len(term) > 24 || queryStopWords[term] || strings.IndexFunc(term, func(r rune) bool { return !unicode.IsLetter(r) }) >= 0 || strings.Contains("|document|documents|file|files|page|pages|txt|pdf|png|jpg|jpeg|csv|json|md|copy|scan|scanned|", "|"+term+"|") {
				continue
			}
			scores[term] += (1 + math.Log(float64(count))) * (1 + math.Log(float64(len(docs)+1)/float64(frequency[term]+1))) * float64(1+min(1, name[term]))
		}
	}
	terms := make([]string, 0, len(scores))
	for term := range scores {
		terms = append(terms, term)
	}
	sort.Slice(terms, func(i, j int) bool {
		if scores[terms[i]] == scores[terms[j]] {
			return terms[i] < terms[j]
		}
		return scores[terms[i]] > scores[terms[j]]
	})
	name := []string{}
	for _, term := range terms[:min(2, len(terms))] {
		r, size := utf8.DecodeRuneInString(term)
		name = append(name, string(unicode.ToUpper(r))+term[size:])
	}
	if len(name) == 0 {
		return "New tag"
	}
	return strings.Join(name, " ")
}
