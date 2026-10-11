package desktop

import (
	"errors"

	"klefki/core/search"
	vs "klefki/core/sync"
)

type OrganizationPlan struct {
	Generation uint64 `json:"generation"`
	search.Organization
}

func (s *Service) SuggestOrganization(custom []string) (out OrganizationPlan, err error) {
	if len(custom) > 64 {
		return out, ErrInvalid
	}
	for _, tag := range custom {
		if !vs.ValidUserTag(tag) {
			return out, ErrInvalid
		}
	}
	err = s.with(func(x *session) error {
		p := s.semanticPreferences(x)
		if !p.Enabled {
			return errors.New("SEMANTIC_DISABLED")
		}
		model, e := s.semanticModel(x)
		if e != nil {
			return e
		}
		ctx, done := s.semanticContext(x, x.ctx)
		defer done()
		plan, e := search.SuggestTags(ctx, x.store, x.master, model, custom, p.Thresholds)
		if errors.Is(e, search.ErrStale) {
			_, e = search.BuildSemantic(ctx, x.store, x.master, search.Extractor{}, model)
			if e == nil {
				s.changed(x)
				plan, e = search.SuggestTags(ctx, x.store, x.master, model, custom, p.Thresholds)
			}
		}
		if e == nil {
			out = OrganizationPlan{x.generation, plan}
		}
		return e
	})
	return
}

func (s *Service) ApplyOrganization(generation uint64, changes []vs.TagChange) (count int, err error) {
	err = s.with(func(x *session) error {
		if x.generation != generation {
			return vs.ErrConflict
		}
		heads, e := x.store.Heads(x.master)
		if e != nil {
			return e
		}
		eligible := map[string]Item{}
		for _, item := range items(heads) {
			if item.Type == "file" && !item.Trashed && !item.Conflict {
				eligible[item.Revision] = item
			}
		}
		if len(changes) == 0 || len(changes) > len(eligible) {
			return ErrInvalid
		}
		seen := map[string]bool{}
		for _, change := range changes {
			item, ok := eligible[change.Revision]
			if !ok {
				return vs.ErrConflict
			}
			if !vs.ValidUserTag(change.Tag) || seen[change.Revision] || len(item.Tags) >= 64 {
				return ErrInvalid
			}
			seen[change.Revision] = true
		}
		count, e = x.store.TagFiles(x.ctx, x.master, changes)
		if e == nil && count > 0 {
			s.changed(x)
		}
		return e
	})
	return
}
