package desktop

import (
	"errors"
	"net"
	"time"
	vc "vault/core/crypto"
	vs "vault/core/sync"
)

func (s *Service) Connect(address string, register bool) error {
	return s.with(func(x *session) error {
		if x.recovery {
			return errors.New("RECOVERY_LOCAL_ONLY")
		}
		r, e := vs.NewRemote(address, nil, false)
		if e != nil {
			return e
		}
		// Binding is permanent for a local store; reject a different origin before signup.
		if e = x.store.BindRemote(address); e != nil {
			return e
		}
		var session vs.Session
		if register {
			session, e = r.Signup(x.ctx, x.account, x.credential)
		} else {
			session, e = r.Login(x.ctx, x.account.ID, x.credential)
		}
		if e != nil {
			return e
		}
		if x.remote != nil && x.remoteSession.Token != "" {
			_ = x.remote.Revoke(x.ctx, x.remoteSession)
		}
		x.remote = r
		x.remoteSession = session
		s.mu.Lock()
		s.config.Server = address
		e = s.save()
		s.mu.Unlock()
		if e == nil {
			s.changed(x)
		}
		return e
	})
}
func (s *Service) SyncNow() {
	s.mu.Lock()
	if s.session == nil || s.session.recovery || s.config.Server == "" || s.syncRunning {
		s.mu.Unlock()
		return
	}
	x := s.session
	address := s.config.Server
	s.syncRunning = true
	s.syncState = "Syncing"
	s.mu.Unlock()
	s.event(Event{Generation: x.generation, Kind: "sync"})
	go func() {
		e := s.with(func(current *session) error {
			if current != x {
				return ErrLocked
			}
			if x.remote == nil {
				r, e := vs.NewRemote(address, nil, false)
				if e != nil {
					return e
				}
				session, e := r.Login(x.ctx, x.account.ID, x.credential)
				if e != nil {
					return e
				}
				x.remote = r
				x.remoteSession = session
			}
			c := vs.Client{Store: x.store, Remote: x.remote, Session: x.remoteSession, Master: x.master}
			defer clear(c.Master[:])
			return c.Sync(x.ctx)
		})
		s.mu.Lock()
		s.syncRunning = false
		if s.session != x {
			s.mu.Unlock()
			return
		}
		if e == nil {
			s.syncState = "Synced"
			s.lastSync = time.Now().Format(time.RFC3339)
		} else {
			var he *vs.HTTPError
			if errors.As(e, &he) && (he.Status == 401 || he.Status == 403) {
				s.syncState = "Authentication required"
			} else if errors.As(e, &he) {
				s.syncState = "Sync error"
			} else if errors.Is(e, vs.ErrInvalid) || errors.Is(e, vs.ErrConflict) || errors.Is(e, vc.ErrInvalid) {
				s.syncState = "Sync error"
			} else {
				var networkError net.Error
				if errors.As(e, &networkError) {
					s.syncState = "Offline"
				} else {
					s.syncState = "Sync error"
				}
			}
		}
		s.mu.Unlock()
		_ = s.with(func(current *session) error {
			if current != x {
				return ErrLocked
			}
			n, _ := x.store.Pending()
			s.mu.Lock()
			if s.session == x {
				s.pending = n
				if e == nil && n > 0 {
					s.syncState = "Pending"
				}
			}
			s.mu.Unlock()
			return nil
		})
		s.event(Event{Generation: x.generation, Kind: "library"})
	}()
}
