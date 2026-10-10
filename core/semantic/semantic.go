// Package semantic runs optional local document analysis. It never receives klefki keys.
package semantic

import (
	"context"
	"errors"
	"math"
)

const Dimension = 384
const Window = 480
const Overlap = 64
const MaxTokens = 510

var ErrUnavailable = errors.New("SEMANTIC_UNAVAILABLE")
var ErrModel = errors.New("SEMANTIC_MODEL_INVALID")
var ErrLimit = errors.New("SEARCH_LIMIT")

type Embedder interface {
	Fingerprint() string
	Tokens(string) ([]int64, error)
	EmbedTokens(context.Context, []int64) ([]float32, error)
	Close()
}

type Thresholds struct {
	Cutoff float64 `json:"cutoff"`
	Margin float64 `json:"margin"`
}

// DefaultThresholds were selected on the public synthetic 60-document evaluation.
var DefaultThresholds = Thresholds{0.50, 0.01}

func (t Thresholds) Valid() bool {
	return !math.IsNaN(t.Cutoff) && !math.IsNaN(t.Margin) && t.Cutoff >= -1 && t.Cutoff <= 1 && t.Margin >= 0 && t.Margin <= 2
}

type Label struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

var Labels = []Label{
	{"Identity", "Personal identification documents, such as passports, national ID cards, and driving licences."},
	{"Money", "Financial records, such as bills, invoices, receipts, bank statements, and tax documents."},
	{"Legal", "Legal rights and obligations, such as contracts, agreements, deeds, and court documents."},
	{"Health", "Medical and healthcare records, such as prescriptions, laboratory reports, vaccination records, and discharge summaries."},
	{"Education", "Academic and learning records, such as diplomas, transcripts, examination results, and course certificates."},
	{"Work", "Employment and professional records, such as offer letters, employment certificates, performance reviews, and workplace correspondence."},
}

func ValidCategory(s string) bool {
	if s == "" {
		return true
	}
	for _, l := range Labels {
		if s == l.Name {
			return true
		}
	}
	return false
}

func Normalize(v []float32) bool {
	n := 0.0
	for _, x := range v {
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
			return false
		}
		n += float64(x) * float64(x)
	}
	if n < 1e-12 {
		return false
	}
	n = math.Sqrt(n)
	for i := range v {
		v[i] = float32(float64(v[i]) / n)
	}
	return true
}

func ValidVector(v []float32) bool {
	if len(v) != Dimension {
		return false
	}
	n := 0.0
	for _, x := range v {
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
			return false
		}
		n += float64(x) * float64(x)
	}
	return math.Abs(n-1) < 0.002
}

func Cosine(a, b []float32) float64 {
	if len(a) != len(b) {
		return -1
	}
	n := 0.0
	for i := range a {
		n += float64(a[i]) * float64(b[i])
	}
	return math.Max(-1, math.Min(1, n))
}

func Embed(ctx context.Context, e Embedder, text string) ([]float32, error) {
	t, err := e.Tokens(text)
	if err != nil {
		return nil, err
	}
	if len(t) > Window {
		t = t[:Window]
	}
	return e.EmbedTokens(ctx, t)
}

func Chunks(ctx context.Context, e Embedder, text string) ([][]float32, error) {
	t, err := e.Tokens(text)
	if err != nil {
		return nil, err
	}
	defer clear(t)
	out := [][]float32{}
	for start := 0; start < len(t); start += Window - Overlap {
		end := min(start+Window, len(t))
		v, err := e.EmbedTokens(ctx, t[start:end])
		if err != nil {
			for _, v := range out {
				clear(v)
			}
			return nil, err
		}
		if !ValidVector(v) {
			return nil, ErrModel
		}
		out = append(out, v)
		if end == len(t) {
			break
		}
	}
	return out, nil
}

func Mean(vectors [][]float32) []float32 {
	v := make([]float32, Dimension)
	for _, x := range vectors {
		for i := range v {
			v[i] += x[i]
		}
	}
	if !Normalize(v) {
		return nil
	}
	return v
}

type Prediction struct {
	Category string  `json:"category"`
	Score    float64 `json:"score"`
	Margin   float64 `json:"margin"`
	Method   string  `json:"method"`
}

func Classify(vector []float32, labels [][]float32, t Thresholds) Prediction {
	p := Prediction{Method: "zero-shot"}
	if !ValidVector(vector) || !t.Valid() || len(labels) != len(Labels) {
		return p
	}
	best, second := -2.0, -2.0
	name := ""
	for i, v := range labels {
		score := Cosine(vector, v)
		if score > best {
			second, best, name = best, score, Labels[i].Name
		} else if score > second {
			second = score
		}
	}
	p.Score, p.Margin = best, best-second
	if best >= t.Cutoff && p.Margin >= t.Margin {
		p.Category = name
	}
	return p
}
