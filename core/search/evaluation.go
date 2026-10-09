package search

import (
	"context"
	vc "vault/core/crypto"
	"vault/core/semantic"
	vs "vault/core/sync"
)

type LabelledQuery struct {
	Query    string   `json:"query"`
	Expected []string `json:"expected"`
}
type LabelledCategory struct {
	Revision string `json:"revision"`
	Category string `json:"category"`
}
type EvaluationSet struct {
	Queries    []LabelledQuery    `json:"queries"`
	Categories []LabelledCategory `json:"categories"`
}
type Metrics struct {
	Top3 float64 `json:"top3"`
	MRR  float64 `json:"mrr"`
}
type ClassificationMetrics struct {
	Accuracy  float64 `json:"accuracy"`
	Coverage  float64 `json:"coverage"`
	Precision float64 `json:"precision"`
	Correct   int     `json:"correct"`
	Wrong     int     `json:"wrong"`
	Abstained int     `json:"abstained"`
}
type Evaluation struct {
	RankingDepth   int                   `json:"rankingDepth"`
	Documents      int                   `json:"documents"`
	Queries        int                   `json:"queries"`
	Modes          map[string]Metrics    `json:"modes"`
	Thresholds     semantic.Thresholds   `json:"thresholds"`
	Classification ClassificationMetrics `json:"classification"`
	HybridPass     bool                  `json:"hybridPass"`
}

func Evaluate(ctx context.Context, s *vs.Store, key vc.Key, model semantic.Embedder, labels EvaluationSet) (Evaluation, error) {
	defer vc.Wipe(key[:])
	out := Evaluation{RankingDepth: 20, Queries: len(labels.Queries), Modes: map[string]Metrics{}}
	heads, e := s.Heads(key)
	if e != nil {
		return out, e
	}
	docs := documents(heads)
	out.Documents = len(docs)
	known := map[string]bool{}
	for _, d := range docs {
		known[d.Revision] = true
	}
	if len(labels.Queries) < 100 || len(docs) < 50 || len(labels.Categories) == 0 {
		return out, ErrIndex
	}
	for _, q := range labels.Queries {
		if len(q.Expected) == 0 || len(q.Query) > 4096 {
			return out, ErrIndex
		}
		for _, id := range q.Expected {
			if !known[id] {
				return out, ErrStale
			}
		}
	}
	for _, mode := range []string{"keyword", "semantic", "hybrid"} {
		metric := Metrics{}
		for _, q := range labels.Queries {
			hits, e := QueryMode(ctx, s, key, model, q.Query, mode)
			if e != nil {
				return out, e
			}
			hits = hits[:min(20, len(hits))]
			found := false
			for i, h := range hits {
				for _, id := range q.Expected {
					if h.Revision == id {
						if i < 3 {
							metric.Top3++
						}
						metric.MRR += 1 / float64(i+1)
						found = true
						break
					}
				}
				if found {
					break
				}
			}
		}
		metric.Top3 /= float64(len(labels.Queries))
		metric.MRR /= float64(len(labels.Queries))
		out.Modes[mode] = metric
	}
	ix, e := loadSemantic(ctx, s, key, heads, docs, model.Fingerprint())
	if e != nil {
		return out, e
	}
	defer ix.Clear()
	seen := map[string]bool{}
	for _, l := range labels.Categories {
		if !known[l.Revision] || !semantic.ValidCategory(l.Category) || l.Category == "" || seen[l.Revision] {
			return out, ErrIndex
		}
		seen[l.Revision] = true
	}
	best := -1.0
	for _, cutoff := range []float64{.20, .25, .30, .35, .40, .45, .50, .55, .60, .65, .70, .75, .80, .85, .90} {
		for _, margin := range []float64{0, .01, .02, .03, .05, .075, .10} {
			t := semantic.Thresholds{Cutoff: cutoff, Margin: margin}
			m := ClassificationMetrics{}
			for _, l := range labels.Categories {
				for _, d := range ix.Documents {
					if d.Revision == l.Revision {
						p := semantic.Classify(d.Vector, ix.Labels, t)
						if p.Category == "" {
							m.Abstained++
						} else if p.Category == l.Category {
							m.Correct++
						} else {
							m.Wrong++
						}
						break
					}
				}
			}
			n := float64(len(labels.Categories))
			m.Accuracy = float64(m.Correct) / n
			m.Coverage = float64(m.Correct+m.Wrong) / n
			if m.Correct+m.Wrong > 0 {
				m.Precision = float64(m.Correct) / float64(m.Correct+m.Wrong)
			}
			// Micro F1 counts abstentions as misses; ties prefer fewer errors, a higher cutoff, then the configured defaults.
			score := 2 * float64(m.Correct) / (n + float64(m.Correct+m.Wrong))
			prefer := score > best || (score == best && (m.Wrong < out.Classification.Wrong || (m.Wrong == out.Classification.Wrong && (t.Cutoff > out.Thresholds.Cutoff || (t.Cutoff == out.Thresholds.Cutoff && t == semantic.DefaultThresholds)))))
			if prefer {
				best = score
				out.Thresholds = t
				out.Classification = m
			}
		}
	}
	out.HybridPass = out.Modes["hybrid"].Top3 >= .8
	return out, nil
}
