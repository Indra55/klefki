package desktop

import (
	"errors"
	vs "klefki/core/sync"
)

func (s *Service) contentClient(x *session) (*vs.Client, error) {
	s.mu.Lock()
	remote, auth := x.remote, x.remoteSession
	s.mu.Unlock()
	if x.recovery {
		return nil, errors.New("RECOVERY_LOCAL_ONLY")
	}
	if remote == nil {
		return nil, errors.New("SYNC_CONNECT_REQUIRED")
	}
	return &vs.Client{Store: x.store, Remote: remote, Session: auth, Master: x.master}, nil
}
func (s *Service) SetTrashOverride(value string) error {
	e := s.with(func(x *session) error { return x.store.SetTrashOverride(value) })
	if e == nil {
		s.SyncNow()
	}
	return e
}
func (s *Service) SetTrashDefault(enabled bool) error {
	e := s.with(func(x *session) error {
		c, e := s.contentClient(x)
		if e != nil {
			return e
		}
		defer clear(c.Master[:])
		return c.SetTrashDefault(x.ctx, enabled)
	})
	if e == nil {
		s.SyncNow()
	}
	return e
}
