package desktop

import (
	"context"
	"errors"
	"fmt"
	"time"
	"vault/core/search"
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
	report, e := search.Build(ctx, x.store, x.master, search.Extractor{})
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
