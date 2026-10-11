package desktop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"time"

	vc "klefki/core/crypto"
	"klefki/core/local"
	vs "klefki/core/sync"
)

var (
	ErrFingerprintUnavailable = errors.New("FINGERPRINT_UNAVAILABLE")
	ErrFingerprintFailed      = errors.New("FINGERPRINT_FAILED")
	ErrFingerprintDisabled    = errors.New("FINGERPRINT_DISABLED")
	ErrFingerprintMissing     = errors.New("FINGERPRINT_MISSING")
	ErrFingerprintPassword    = errors.New("FINGERPRINT_PASSWORD_REQUIRED")
	ErrFingerprintKeyring     = errors.New("FINGERPRINT_KEYRING")
	ErrFingerprintBusy        = errors.New("FINGERPRINT_BUSY")
	ErrFingerprintCanceled    = errors.New("FINGERPRINT_CANCELED")
)

// FingerprintProvider performs OS verification and delegates persistent secrets to the OS keyring.
type FingerprintProvider interface {
	Available(context.Context) error
	Verify(context.Context, func(string)) error
	Store(context.Context, string, []byte) error
	Load(context.Context, string) ([]byte, error)
	Delete(context.Context, string) error
}
type FingerprintState struct {
	Supported bool   `json:"supported"`
	Available bool   `json:"available"`
	Enabled   bool   `json:"enabled"`
	Reason    string `json:"reason"`
}
type fingerprintRecord struct {
	Version    int    `json:"version"`
	Binding    string `json:"binding"`
	Master     []byte `json:"master"`
	Credential []byte `json:"credential"`
}

func accountBinding(a vc.Account) string {
	hash := sha256.Sum256(local.AccountBytes(a))
	return a.ID.String() + "-" + hex.EncodeToString(hash[:])
}
func (s *Service) SetFingerprintProvider(p FingerprintProvider) {
	s.mu.Lock()
	s.fingerprint = p
	s.fingerprintState.Supported = p != nil
	s.mu.Unlock()
}
func (s *Service) RefreshFingerprint() FingerprintState {
	s.mu.Lock()
	p := s.fingerprint
	s.mu.Unlock()
	reason := ""
	ready := false
	if p != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		e := p.Available(ctx)
		cancel()
		ready = e == nil
		reason = Code(e)
	}
	s.mu.Lock()
	s.fingerprintState.Available = ready
	s.fingerprintState.Reason = reason
	cfg := s.config
	st := s.fingerprintState
	s.mu.Unlock()
	if a, e := local.LoadAccount(cfg.AccountPath); e == nil {
		st.Enabled = slices.Contains(cfg.FingerprintAccounts, accountBinding(a))
	}
	return st
}
func (s *Service) EnableFingerprint() error {
	e := s.with(func(x *session) error {
		if x.recovery {
			return ErrFingerprintPassword
		}
		s.mu.Lock()
		p := s.fingerprint
		s.mu.Unlock()
		if p == nil {
			return ErrFingerprintUnavailable
		}
		ctx, cancel := context.WithTimeout(x.ctx, 45*time.Second)
		s.mu.Lock()
		s.fingerprintCancel = cancel
		s.mu.Unlock()
		defer func() { cancel(); s.mu.Lock(); s.fingerprintCancel = nil; s.mu.Unlock() }()
		if e := p.Available(ctx); e != nil {
			return e
		}
		if e := p.Verify(ctx, func(status string) { s.event(Event{Generation: x.generation, Kind: "fingerprint", Name: status}) }); e != nil {
			return e
		}
		binding := accountBinding(x.account)
		record := fingerprintRecord{1, binding, x.master[:], x.credential[:]}
		data, e := json.Marshal(record)
		if e != nil {
			return ErrFingerprintKeyring
		}
		defer vc.Wipe(data)
		if e = p.Store(ctx, binding, data); e != nil {
			return e
		}
		s.mu.Lock()
		if s.session != x || ctx.Err() != nil {
			s.mu.Unlock()
			cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
			_ = p.Delete(cleanup, binding)
			stop()
			return ErrLocked
		}
		previous := s.config.FingerprintAccounts
		s.config.FingerprintAccounts = slices.Clone(previous)
		if !slices.Contains(previous, binding) {
			s.config.FingerprintAccounts = append(s.config.FingerprintAccounts, binding)
		}
		e = s.save()
		if e != nil {
			s.config.FingerprintAccounts = previous
		}
		s.fingerprintState.Available = true
		s.mu.Unlock()
		return e
	})
	if errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded) {
		return ErrFingerprintCanceled
	}
	return e
}
func (s *Service) DisableFingerprint() error {
	return s.with(func(x *session) error {
		s.mu.Lock()
		p := s.fingerprint
		s.mu.Unlock()
		if p == nil {
			return ErrFingerprintUnavailable
		}
		binding := accountBinding(x.account)
		ctx, cancel := context.WithTimeout(x.ctx, 15*time.Second)
		defer cancel()
		if e := p.Delete(ctx, binding); e != nil {
			return e
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.config.FingerprintAccounts = slices.DeleteFunc(slices.Clone(s.config.FingerprintAccounts), func(id string) bool { return id == binding })
		return s.save()
	})
}
func (s *Service) CancelFingerprint() {
	s.mu.Lock()
	if s.fingerprintCancel != nil {
		s.fingerprintCancel()
	}
	s.mu.Unlock()
}
func (s *Service) UnlockFingerprint() (result error) {
	defer func() {
		if errors.Is(result, context.Canceled) || errors.Is(result, context.DeadlineExceeded) {
			result = ErrFingerprintCanceled
		}
	}()
	s.work.Lock()
	defer s.work.Unlock()
	s.mu.Lock()
	if s.session != nil {
		s.mu.Unlock()
		return nil
	}
	cfg := s.config
	p := s.fingerprint
	generation := s.generation
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	s.fingerprintCancel = cancel
	s.mu.Unlock()
	defer func() { cancel(); s.mu.Lock(); s.fingerprintCancel = nil; s.mu.Unlock() }()
	if p == nil {
		return ErrFingerprintUnavailable
	}
	a, e := local.LoadAccount(cfg.AccountPath)
	if e != nil {
		return e
	}
	binding := accountBinding(a)
	if !slices.Contains(cfg.FingerprintAccounts, binding) {
		return ErrFingerprintDisabled
	}
	if e = p.Available(ctx); e != nil {
		return e
	}
	if e = p.Verify(ctx, func(status string) { s.event(Event{Generation: generation, Kind: "fingerprint", Name: status}) }); e != nil {
		return e
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	data, e := p.Load(ctx, binding)
	if e != nil {
		return e
	}
	defer vc.Wipe(data)
	var record fingerprintRecord
	if len(data) > 1024 || json.Unmarshal(data, &record) != nil {
		return ErrFingerprintMissing
	}
	defer vc.Wipe(record.Master)
	defer vc.Wipe(record.Credential)
	if record.Version != 1 || record.Binding != binding || len(record.Master) != 32 || len(record.Credential) != 32 {
		return ErrFingerprintMissing
	}
	var master, credential vc.Key
	copy(master[:], record.Master)
	copy(credential[:], record.Credential)
	defer vc.Wipe(master[:])
	defer vc.Wipe(credential[:])
	return s.startSession(ctx, cfg, a, master, credential, false, generation)
}
func (s *Service) startSession(ctx context.Context, cfg Config, a vc.Account, master, credential vc.Key, recovery bool, generation uint64) error {
	defer vc.Wipe(master[:])
	defer vc.Wipe(credential[:])
	st, e := vs.Open(cfg.StorePath, a.ID)
	if e != nil {
		return e
	}
	if _, e = st.Heads(master); e != nil {
		st.Close()
		return e
	}
	relay, e := st.RelayState()
	if e != nil {
		st.Close()
		return e
	}
	sessionCtx, cancel := context.WithCancel(context.Background())
	n, _ := st.Pending()
	s.mu.Lock()
	if s.generation != generation || ctx.Err() != nil {
		s.mu.Unlock()
		cancel()
		st.Close()
		return ErrLocked
	}
	s.generation++
	s.session = &session{account: a, master: master, credential: credential, recovery: recovery, store: st, ctx: sessionCtx, cancel: cancel, generation: s.generation}
	s.lastActivity = time.Now()
	s.pending = n
	s.syncMode, s.syncDevice, s.syncDevices = relay.Mode, relay.Device, relay.Known
	s.syncState = "Local only"
	s.mu.Unlock()
	go s.SyncNow()
	return nil
}
