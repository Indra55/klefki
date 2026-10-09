package desktop

import (
	"context"
	"errors"
	"fmt"
	"time"
	"vault/core/search"
	"vault/core/semantic"
)

type SearchResults struct {
	Generation   uint64       `json:"generation"`
	Hits         []search.Hit `json:"hits"`
	Mode         string       `json:"mode"`
	Availability string       `json:"availability"`
}

func (s *Service) Search(query string) (out SearchResults, e error) {
	e = s.with(func(x *session) error {
		out = SearchResults{Generation: x.generation, Mode: "keyword", Availability: "disabled"}
		if s.semanticPreferences(x).Enabled {
			model, err := s.semanticModel(x)
			if err == nil {
				ctx, done := s.semanticContext(x, x.ctx)
				defer done()
				hits, err := search.QueryMode(ctx, x.store, x.master, model, query, "hybrid")
				if errors.Is(err, search.ErrStale) {
					_, err = search.BuildSemantic(ctx, x.store, x.master, search.Extractor{}, model)
					if err == nil {
						s.changed(x)
						hits, err = search.QueryMode(ctx, x.store, x.master, model, query, "hybrid")
					}
				}
				if err == nil {
					out.Hits = hits
					out.Mode = "hybrid"
					out.Availability = "ready"
					return nil
				}
				out.Availability = Code(err)
			} else {
				out.Availability = Code(err)
			}
		}
		hits, err := search.Query(x.ctx, x.store, x.master, query)
		if errors.Is(err, search.ErrStale) {
			if _, err = search.Build(x.ctx, x.store, x.master, search.Extractor{}); err != nil {
				return err
			}
			s.changed(x)
			hits, err = search.Query(x.ctx, x.store, x.master, query)
		}
		if err != nil {
			return err
		}
		out.Hits = hits
		return nil
	})
	return
}

func (s *Service) RebuildIndex() (string, error) {
	return s.operation(func(x *session, ctx context.Context, op string) error {
		return s.buildIndex(x, ctx, op)
	})
}

func (s *Service) buildIndex(x *session, ctx context.Context, op string) error {
	s.event(Event{Generation: x.generation, Kind: "index-start", Operation: op})
	started := time.Now()
	var report search.Report
	var e error
	if s.semanticPreferences(x).Enabled {
		model, err := s.semanticModel(x)
		if err == nil {
			semanticCtx, done := s.semanticContext(x, ctx)
			report, e = search.BuildSemantic(semanticCtx, x.store, x.master, search.Extractor{}, model)
			done()
		} else {
			report, e = search.Build(ctx, x.store, x.master, search.Extractor{})
			s.event(Event{Generation: x.generation, Kind: "semantic-status", Error: Code(err)})
		}
	} else {
		report, e = search.Build(ctx, x.store, x.master, search.Extractor{})
	}
	if e != nil && (errors.Is(e, semantic.ErrModel) || errors.Is(e, semantic.ErrUnavailable)) {
		s.event(Event{Generation: x.generation, Kind: "semantic-status", Error: Code(e)})
		report, e = search.Build(ctx, x.store, x.master, search.Extractor{})
	}
	elapsed := float64(time.Since(started)) / float64(time.Millisecond)
	if e == nil {
		e = check(ctx)
	}
	event := Event{Generation: x.generation, Kind: "index-finished", Operation: op, DurationMS: elapsed, Error: Code(e)}
	if ctx.Err() != nil {
		event.Error = "CANCELED"
	}
	if e == nil {
		event.Name = fmt.Sprintf("%d documents indexed; %d with names and tags only", report.Documents, report.MetadataOnly)
		s.changed(x)
	}
	s.event(event)
	return e
}
