package desktop

import (
	"context"
	"errors"
	"path/filepath"
	"vault/core/search"
	"vault/core/semantic"
)

type SemanticPreferences struct {
	Enabled          bool                `json:"enabled"`
	Thresholds       semantic.Thresholds `json:"thresholds"`
	ModelFingerprint string              `json:"modelFingerprint,omitempty"`
}
type SemanticState struct {
	SemanticPreferences
	Supported     bool  `json:"supported"`
	Ready         bool  `json:"ready"`
	DownloadBytes int64 `json:"downloadBytes"`
}

func cloneSemantic(m map[string]SemanticPreferences) map[string]SemanticPreferences {
	out := map[string]SemanticPreferences{}
	for k, v := range m {
		out[k] = v
	}
	return out
}
func (s *Service) modelRoot() string {
	return filepath.Join(filepath.Dir(s.configPath), "models", semantic.Fingerprint())
}
func (s *Service) semanticPreferences(x *session) SemanticPreferences {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.config.Semantic[accountBinding(x.account)]
	if !ok || p.ModelFingerprint != semantic.Fingerprint() {
		p.Thresholds = semantic.DefaultThresholds
		p.ModelFingerprint = semantic.Fingerprint()
	}
	return p
}
func (s *Service) semanticModel(x *session) (semantic.Embedder, error) {
	if x.model != nil {
		return x.model, nil
	}
	m, e := semantic.Open(s.modelRoot())
	if e == nil {
		x.model = m
	}
	return m, e
}
func (s *Service) semanticContext(x *session, parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	s.mu.Lock()
	x.semanticCancel = cancel
	s.mu.Unlock()
	return ctx, func() { cancel(); s.mu.Lock(); x.semanticCancel = nil; s.mu.Unlock() }
}
func (s *Service) SemanticStatus() (out SemanticState, e error) {
	e = s.with(func(x *session) error {
		out = SemanticState{SemanticPreferences: s.semanticPreferences(x), Supported: semantic.Supported(), Ready: semantic.Verify(s.modelRoot()) == nil, DownloadBytes: semantic.DownloadSize()}
		return nil
	})
	return
}
func (s *Service) ConfigureSemantic(p SemanticPreferences) error {
	p.ModelFingerprint = semantic.Fingerprint()
	if !p.Thresholds.Valid() {
		return ErrInvalid
	}
	if !p.Enabled {
		s.mu.Lock()
		if x := s.session; x != nil && x.semanticCancel != nil {
			x.semanticCancel()
		}
		s.mu.Unlock()
	}
	return s.with(func(x *session) error {
		if p.Enabled && (!semantic.Supported() || semantic.Verify(s.modelRoot()) != nil) {
			return semantic.ErrUnavailable
		}
		if !p.Enabled && x.model != nil {
			x.model.Close()
			x.model = nil
		}
		s.mu.Lock()
		s.config.Semantic = cloneSemantic(s.config.Semantic)
		s.config.Semantic[accountBinding(x.account)] = p
		e := s.save()
		s.mu.Unlock()
		return e
	})
}
func (s *Service) SetupSemantic(bundle string) (string, error) {
	return s.operation(func(x *session, parent context.Context, op string) error {
		ctx, done := s.semanticContext(x, parent)
		defer done()
		s.event(Event{Generation: x.generation, Kind: "file-start", Operation: op, Name: "Setting up local model"})
		if e := semantic.Install(ctx, s.modelRoot(), bundle, func(n, total int64) {
			s.event(Event{Generation: x.generation, Kind: "progress", Operation: op, Done: uint64(n), Total: uint64(total)})
		}); e != nil {
			return e
		}
		if e := ctx.Err(); e != nil {
			return e
		}
		p := s.semanticPreferences(x)
		p.Enabled = true
		s.mu.Lock()
		s.config.Semantic = cloneSemantic(s.config.Semantic)
		s.config.Semantic[accountBinding(x.account)] = p
		e := s.save()
		s.mu.Unlock()
		if e != nil {
			return e
		}
		s.event(Event{Generation: x.generation, Kind: "semantic-status"})
		s.changed(x)
		return s.buildIndex(x, parent, op)
	})
}

type DocumentAnalysis struct {
	Generation uint64 `json:"generation"`
	search.Analysis
}

func (s *Service) Analyze(revision string) (out DocumentAnalysis, e error) {
	e = s.with(func(x *session) error {
		p := s.semanticPreferences(x)
		if !p.Enabled {
			return errors.New("SEMANTIC_DISABLED")
		}
		a, e := search.Analyze(x.ctx, x.store, x.master, revision, p.Thresholds)
		if e != nil {
			return e
		}
		out = DocumentAnalysis{x.generation, a}
		return nil
	})
	return
}
func (s *Service) CorrectAnalysis(revision, category string, fields []semantic.Field, resolve []string) (id string, e error) {
	e = s.with(func(x *session) error {
		if !s.semanticPreferences(x).Enabled {
			return errors.New("SEMANTIC_DISABLED")
		}
		var err error
		id, err = search.SaveCorrection(x.ctx, x.store, x.master, revision, category, fields, resolve)
		if err == nil {
			s.changed(x)
		}
		return err
	})
	return
}
