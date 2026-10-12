package desktop

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"
	"unicode/utf8"

	vc "klefki/core/crypto"
	vs "klefki/core/sync"
	"klefki/core/syncwire"
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
			local, err := x.store.RelayState()
			if err != nil {
				return err
			}
			session, e = r.LoginAccount(x.ctx, x.account, x.credential, local.Mode == syncwire.TemporaryRelay)
		}
		if e != nil {
			return e
		}
		s.mu.Lock()
		previousRemote, previousSession := x.remote, x.remoteSession
		s.mu.Unlock()
		if previousRemote != nil && previousSession.Token != "" {
			_ = previousRemote.Revoke(x.ctx, previousSession)
		}
		s.mu.Lock()
		x.remote = r
		x.remoteSession = session
		s.syncInfo.LastContact = time.Now().UnixMilli()
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
	if s.session == nil || s.session.recovery || s.config.Server == "" {
		s.mu.Unlock()
		return
	}
	x := s.session
	if s.syncRunning && s.syncOwner == x {
		s.syncAgain = true
		s.mu.Unlock()
		return
	}
	address := s.config.Server
	s.syncRunning, s.syncOwner, s.syncAgain = true, x, false
	s.syncState = "Syncing"
	s.syncInfo.Phase, s.syncInfo.Running, s.syncInfo.Progress = "connecting", true, nil
	s.mu.Unlock()
	s.emitSync(x)
	go func() {
		e := s.with(func(current *session) error {
			if current != x {
				return ErrLocked
			}
			s.mu.Lock()
			remote, session := x.remote, x.remoteSession
			s.mu.Unlock()
			if remote == nil {
				r, err := vs.NewRemote(address, nil, false)
				if err != nil {
					return err
				}
				local, err := x.store.RelayState()
				if err != nil {
					return err
				}
				session, err = r.LoginAccount(x.ctx, x.account, x.credential, local.Mode == syncwire.TemporaryRelay)
				if err != nil {
					return err
				}
				remote = r
				s.mu.Lock()
				x.remote, x.remoteSession = remote, session
				s.mu.Unlock()
			}
			c := vs.Client{Store: x.store, Remote: remote, Session: session, Master: x.master, Observe: func(p syncwire.Progress) { s.observeSync(x, p) }, OnContact: func() {
				s.mu.Lock()
				if s.session == x {
					s.syncInfo.LastContact = time.Now().UnixMilli()
				}
				s.mu.Unlock()
			}}
			defer clear(c.Master[:])
			err := c.SyncAccount(x.ctx, x.account, x.credential)
			s.mu.Lock()
			x.remoteSession = c.Session
			s.mu.Unlock()
			if local, e := x.store.RelayState(); e == nil {
				s.mu.Lock()
				if s.session == x {
					s.syncMode, s.syncDevices, s.syncDevice = local.Mode, local.Known, local.Device
				}
				s.mu.Unlock()
			}
			if err == nil {
				if settings, e := c.Settings(x.ctx); e == nil && settings.Mode == syncwire.TemporaryRelay && settings.Queued > 0 {
					return errors.New("RELAY_QUEUED")
				}
			}
			return err
		})
		s.mu.Lock()
		if s.syncOwner == x {
			s.syncRunning = false
		}
		if s.session != x {
			s.mu.Unlock()
			return
		}
		again := s.syncAgain
		s.syncAgain = false
		s.syncInfo.Running = false
		s.syncInfo.Phase, s.syncState = syncFailure(e)
		if e != nil && s.syncInfo.Phase != "waiting" && s.syncInfo.Progress != nil {
			p := *s.syncInfo.Progress
			p.Phase, p.UpdatedAt = s.syncInfo.Phase, time.Now().UnixMilli()
			s.syncInfo.Progress = &p
		}
		if e == nil || e.Error() == "RELAY_QUEUED" {
			s.lastSync = time.Now().Format(time.RFC3339)
		}
		if s.syncInfo.Phase == "auth" {
			x.remote = nil
			x.remoteSession = vs.Session{}
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
					s.syncState, s.syncInfo.Phase = "Pending", "pending"
				}
			}
			s.mu.Unlock()
			return nil
		})
		s.emitSync(x)
		s.event(Event{Generation: x.generation, Kind: "library"})
		select {
		case s.monitorWake <- struct{}{}:
		default:
		}
		if again && e == nil {
			s.SyncNow()
		}
	}()
}

func syncFailure(err error) (string, string) {
	if err == nil {
		return "synced", "Synced"
	}
	var he *vs.HTTPError
	var ne net.Error
	switch {
	case err.Error() == "RELAY_QUEUED", errors.Is(err, vs.ErrRelayWaiting):
		return "waiting", "Waiting for linked devices"
	case errors.As(err, &he):
		switch he.Status {
		case 401, 403:
			return "auth", "Authentication required"
		case 507:
			return "full", "Relay storage full"
		case 408, 504:
			return "timeout", "Connection timed out"
		}
	case errors.As(err, &ne):
		if ne.Timeout() {
			return "timeout", "Connection timed out"
		}
		return "unreachable", "Server unreachable"
	case errors.Is(err, vs.ErrRemote):
		return "unreachable", "Server unreachable"
	}
	return "error", "Sync error"
}

func (s *Service) emitSync(x *session) {
	s.mu.Lock()
	if s.session != x {
		s.mu.Unlock()
		return
	}
	info := s.syncInfo
	s.mu.Unlock()
	s.event(Event{Generation: x.generation, Kind: "sync-status", SyncInfo: &info})
}
func (s *Service) observeSync(x *session, p syncwire.Progress) {
	s.mu.Lock()
	if s.session != x {
		s.mu.Unlock()
		return
	}
	s.syncInfo.Progress, s.syncInfo.Phase = &p, p.Phase
	s.mu.Unlock()
	s.emitSync(x)
	select {
	case s.monitorWake <- struct{}{}:
	default:
	}
}
func (s *Service) SetSyncPanel(open bool) {
	s.mu.Lock()
	s.syncPanel = open
	s.mu.Unlock()
	select {
	case s.monitorWake <- struct{}{}:
	default:
	}
}
func (s *Service) SetDeviceAlias(id, name string) error {
	name = strings.TrimSpace(name)
	if _, err := vc.ParseID(id); err != nil || !utf8.ValidString(name) || len([]rune(name)) > 64 || strings.ContainsAny(name, "\r\n\x00") {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session == nil {
		return ErrLocked
	}
	key := s.session.account.ID.String() + "@" + s.config.Server
	if s.config.DeviceAliases == nil {
		s.config.DeviceAliases = map[string]map[string]string{}
	}
	aliases := s.config.DeviceAliases[key]
	if aliases == nil {
		aliases = map[string]string{}
		s.config.DeviceAliases[key] = aliases
	}
	old, exists := aliases[id]
	if !exists && len(aliases) >= 64 && name != "" {
		return ErrInvalid
	}
	if name == "" {
		delete(aliases, id)
	} else {
		aliases[id] = name
	}
	if err := s.save(); err != nil {
		if exists {
			aliases[id] = old
		} else {
			delete(aliases, id)
		}
		return err
	}
	return nil
}

func (s *Service) monitorSync(x *session) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var lastPresence, lastStatus time.Time
	for {
		select {
		case <-x.ctx.Done():
			return
		case <-ticker.C:
		case <-s.monitorWake:
		}
		s.mu.Lock()
		if s.session != x || x.recovery {
			s.mu.Unlock()
			return
		}
		remote, session, device := x.remote, x.remoteSession, s.syncDevice
		info, panel := s.syncInfo, s.syncPanel
		s.mu.Unlock()
		if remote == nil || device == "" {
			continue
		}
		now := time.Now()
		heartbeatInterval := 15 * time.Second
		if info.Running {
			heartbeatInterval = 5 * time.Second
		}
		if now.Sub(lastPresence) >= heartbeatInterval {
			phase := info.Phase
			switch phase {
			case "synced", "pending", "local", "complete":
				phase = "idle"
			case "auth", "unreachable", "offline", "timeout", "full":
				phase = "error"
			}
			in := syncwire.Presence{Device: device, ClientKind: "desktop", Phase: phase}
			if p := info.Progress; info.Running && p != nil {
				in.Revision, in.Direction, in.DoneBytes, in.TotalBytes = p.Revision, p.Direction, p.DoneBytes, p.TotalBytes
			}
			ctx, cancel := context.WithTimeout(x.ctx, 3*time.Second)
			err := remote.Presence(ctx, session, in)
			cancel()
			lastPresence = now
			if err == nil {
				s.mu.Lock()
				if s.session == x {
					s.syncInfo.LastContact = time.Now().UnixMilli()
				}
				s.mu.Unlock()
			}
		}
		interval := 30 * time.Second
		if panel {
			interval = 5 * time.Second
		}
		if now.Sub(lastStatus) >= interval {
			ctx, cancel := context.WithTimeout(x.ctx, 3*time.Second)
			peers, err := remote.Status(ctx, session)
			cancel()
			lastStatus = now
			s.mu.Lock()
			if s.session != x {
				s.mu.Unlock()
				return
			}
			s.syncInfo.StatusAvailable = err == nil
			if err == nil {
				s.syncInfo.Peers = &peers
				s.syncInfo.LastDeviceCheck = time.Now().UnixMilli()
				s.syncInfo.LastContact = s.syncInfo.LastDeviceCheck
			}
			if err != nil && !vs.StatusUnsupported(err) && !s.syncInfo.Running {
				phase, label := syncFailure(err)
				if phase == "auth" || phase == "unreachable" || phase == "timeout" {
					s.syncInfo.Phase, s.syncState = phase, label
				}
			}
			s.mu.Unlock()
			s.emitSync(x)
		}
	}
}

func (s *Service) ForgetSyncDevice(id string) error {
	e := s.with(func(x *session) error {
		if x.recovery {
			return errors.New("RECOVERY_LOCAL_ONLY")
		}
		s.mu.Lock()
		remote, session := x.remote, x.remoteSession
		s.mu.Unlock()
		if remote == nil {
			return errors.New("SYNC_CONNECT_REQUIRED")
		}
		c := vs.Client{Store: x.store, Remote: remote, Session: session, Master: x.master}
		defer clear(c.Master[:])
		return c.ForgetDevice(x.ctx, id)
	})
	if e == nil {
		s.SyncNow()
	}
	return e
}

func (s *Service) SetSyncMode(mode string) error {
	e := s.with(func(x *session) error {
		if x.recovery {
			return errors.New("RECOVERY_LOCAL_ONLY")
		}
		s.mu.Lock()
		remote, session := x.remote, x.remoteSession
		s.mu.Unlock()
		if remote == nil {
			return errors.New("SYNC_CONNECT_REQUIRED")
		}
		c := vs.Client{Store: x.store, Remote: remote, Session: session, Master: x.master}
		defer clear(c.Master[:])
		if e := c.SetMode(x.ctx, mode); e != nil {
			return e
		}
		s.mu.Lock()
		s.syncMode = mode
		s.mu.Unlock()
		return nil
	})
	if e == nil {
		s.SyncNow()
	}
	return e
}
