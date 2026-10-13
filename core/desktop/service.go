// Package desktop coordinates unlocked sessions without depending on a webview.
package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	vc "klefki/core/crypto"
	"klefki/core/local"
	"klefki/core/semantic"
	vs "klefki/core/sync"
	"klefki/core/syncwire"
)

var ErrLocked = errors.New("LOCKED")
var ErrInvalid = errors.New("INVALID_INPUT")

const TrashTag = "klefki:system:trash"

type Preferences struct {
	Theme       string `json:"theme"`
	IdleMinutes int    `json:"idleMinutes"`
	Density     string `json:"density"`
	DefaultSort string `json:"defaultSort"`
}
type Config struct {
	AccountPath         string                         `json:"accountPath"`
	StorePath           string                         `json:"storePath"`
	Server              string                         `json:"server"`
	FingerprintAccounts []string                       `json:"fingerprintAccounts,omitempty"`
	Preferences         Preferences                    `json:"preferences"`
	Semantic            map[string]SemanticPreferences `json:"semantic,omitempty"`
	DeviceAliases       map[string]map[string]string   `json:"deviceAliases,omitempty"`
}
type State struct {
	Config        Config              `json:"config"`
	Unlocked      bool                `json:"unlocked"`
	Generation    uint64              `json:"generation"`
	AccountID     string              `json:"accountID"`
	RecoveryOnly  bool                `json:"recoveryOnly"`
	Sync          string              `json:"sync"`
	Pending       int                 `json:"pending"`
	LastSync      string              `json:"lastSync"`
	Fingerprint   FingerprintState    `json:"fingerprint"`
	Semantic      SemanticPreferences `json:"semantic"`
	SyncMode      string              `json:"syncMode"`
	SyncDevices   []string            `json:"syncDevices"`
	SyncDevice    string              `json:"syncDevice"`
	SyncInfo      SyncInfo            `json:"syncInfo"`
	DeviceAliases map[string]string   `json:"deviceAliases"`
}
type SyncInfo struct {
	Phase           string             `json:"phase"`
	Running         bool               `json:"running"`
	Progress        *syncwire.Progress `json:"progress,omitempty"`
	LastContact     int64              `json:"lastContact"`
	LastDeviceCheck int64              `json:"lastDeviceCheck"`
	StatusAvailable bool               `json:"statusAvailable"`
	Peers           *syncwire.Status   `json:"peers,omitempty"`
	WaitingFor      string             `json:"waitingFor,omitempty"`
}
type Event struct {
	Generation uint64    `json:"generation"`
	Kind       string    `json:"kind"`
	Name       string    `json:"name,omitempty"`
	Operation  string    `json:"operation,omitempty"`
	Done       uint64    `json:"done,omitempty"`
	Total      uint64    `json:"total,omitempty"`
	Error      string    `json:"error,omitempty"`
	DurationMS float64   `json:"durationMs,omitempty"`
	SyncInfo   *SyncInfo `json:"syncInfo,omitempty"`
}
type session struct {
	account            vc.Account
	master, credential vc.Key
	recovery           bool
	store              *vs.Store
	ctx                context.Context
	cancel             context.CancelFunc
	generation         uint64
	remote             *vs.Remote
	remoteSession      vs.Session
	model              semantic.Embedder
	semanticCancel     context.CancelFunc
	network            sync.WaitGroup
}
type Service struct {
	mu                  sync.Mutex
	work                sync.Mutex
	config              Config
	configPath          string
	session             *session
	generation          uint64
	lastActivity        time.Time
	syncState, lastSync string
	pending             int
	emit                func(Event)
	stop                chan struct{}
	stopOnce            sync.Once
	ops                 map[string]context.CancelFunc
	syncRunning         bool
	syncOwner           *session
	syncAgain           bool
	syncRefresh         bool
	syncStatusRequested bool
	syncPanel           bool
	syncInfo            SyncInfo
	monitorWake         chan struct{}
	syncMode            string
	syncDevices         []string
	syncDevice          string
	syncTimer           *time.Timer
	fingerprint         FingerprintProvider
	fingerprintState    FingerprintState
	fingerprintCancel   context.CancelFunc
}

func New(root string, emit func(Event)) (*Service, error) {
	if os.MkdirAll(root, 0700) != nil {
		return nil, vc.ErrIO
	}
	s := &Service{configPath: filepath.Join(root, "settings.json"), emit: emit, stop: make(chan struct{}), ops: map[string]context.CancelFunc{}, syncState: "Local only", syncInfo: SyncInfo{Phase: "local"}, monitorWake: make(chan struct{}, 1)}
	s.config.Preferences = Preferences{Theme: "dark", IdleMinutes: 5, Density: "comfortable", DefaultSort: "name"}
	if b, e := os.ReadFile(s.configPath); e == nil {
		if json.Unmarshal(b, &s.config) != nil {
			return nil, ErrInvalid
		}
	}
	if !validPreferences(s.config.Preferences) {
		s.config.Preferences = Preferences{Theme: "dark", IdleMinutes: 5, Density: "comfortable", DefaultSort: "name"}
	}
	go s.watch()
	return s, nil
}
func validPreferences(p Preferences) bool {
	return (p.Theme == "dark" || p.Theme == "light" || p.Theme == "system") &&
		(p.IdleMinutes == 1 || p.IdleMinutes == 5 || p.IdleMinutes == 15 || p.IdleMinutes == 30) &&
		(p.Density == "" || p.Density == "comfortable" || p.Density == "compact") &&
		(p.DefaultSort == "" || p.DefaultSort == "name" || p.DefaultSort == "type" || p.DefaultSort == "size")
}
func (s *Service) save() error {
	b, e := json.Marshal(s.config)
	if e != nil {
		return vc.ErrIO
	}
	f, e := os.CreateTemp(filepath.Dir(s.configPath), ".settings-*")
	if e != nil {
		return vc.ErrIO
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, e = f.Write(b); e != nil {
		return vc.ErrIO
	}
	if f.Sync() != nil || f.Close() != nil {
		return vc.ErrIO
	}
	if os.Rename(f.Name(), s.configPath) != nil {
		return vc.ErrIO
	}
	return nil
}
func (s *Service) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := s.config
	cfg.Semantic = cloneSemantic(cfg.Semantic)
	cfg.FingerprintAccounts = slices.Clone(cfg.FingerprintAccounts)
	cfg.DeviceAliases = nil
	fp := s.fingerprintState
	if a, e := local.LoadAccount(cfg.AccountPath); e == nil {
		fp.Enabled = slices.Contains(cfg.FingerprintAccounts, accountBinding(a))
	}
	p := SemanticPreferences{Thresholds: semantic.DefaultThresholds}
	if a, e := local.LoadAccount(cfg.AccountPath); e == nil {
		if v, ok := cfg.Semantic[accountBinding(a)]; ok {
			p = v
		}
	}
	mode := s.syncMode
	if mode == "" {
		mode = "persistent"
	}
	st := State{SyncDevice: s.syncDevice, SyncDevices: slices.Clone(s.syncDevices), SyncMode: mode, Semantic: p, Config: cfg, Fingerprint: fp, Generation: s.generation, Sync: s.syncState, Pending: s.pending, LastSync: s.lastSync}
	st.SyncInfo = s.syncInfo
	st.DeviceAliases = map[string]string{}
	if x := s.session; x != nil {
		st.Unlocked = true
		st.AccountID = x.account.ID.String()
		st.RecoveryOnly = x.recovery
		for id, name := range s.config.DeviceAliases[x.account.ID.String()+"@"+cfg.Server] {
			st.DeviceAliases[id] = name
		}
	}
	return st
}
func (s *Service) Preferences(p Preferences) error {
	if !validPreferences(p) {
		return ErrInvalid
	}
	if p.Density == "" {
		p.Density = "comfortable"
	}
	if p.DefaultSort == "" {
		p.DefaultSort = "name"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.config.Preferences
	s.config.Preferences = p
	if e := s.save(); e != nil {
		s.config.Preferences = previous
		return e
	}
	return nil
}
func (s *Service) event(e Event) {
	if s.emit != nil {
		s.emit(e)
	}
}
func (s *Service) Touch() { s.mu.Lock(); s.lastActivity = time.Now(); s.mu.Unlock() }
func (s *Service) watch() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	ticks := 0
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
			s.mu.Lock()
			expired := s.session != nil && time.Since(s.lastActivity) >= time.Duration(s.config.Preferences.IdleMinutes)*time.Minute
			s.mu.Unlock()
			if expired {
				s.Lock()
			}
			ticks++
			if ticks%3600 == 0 {
				s.cleanupTrash()
			}
			if ticks%60 == 0 {
				s.syncNow(true)
			}
		}
	}
}
func (s *Service) Close() {
	s.stopOnce.Do(func() { close(s.stop) })
	s.Lock()
	s.work.Lock()
	s.work.Unlock()
}
func (s *Service) Lock() {
	s.mu.Lock()
	x := s.session
	if s.fingerprintCancel != nil {
		s.fingerprintCancel()
	}
	if x == nil {
		s.generation++
		s.mu.Unlock()
		return
	}
	s.session = nil
	s.generation++
	g := s.generation
	x.cancel()
	s.pending = 0
	s.syncState = "Locked"
	s.syncInfo = SyncInfo{Phase: "locked"}
	s.syncPanel, s.syncAgain = false, false
	s.syncRefresh, s.syncStatusRequested = false, false
	if s.syncTimer != nil {
		s.syncTimer.Stop()
		s.syncTimer = nil
	}
	s.mu.Unlock()
	s.event(Event{Generation: g, Kind: "locked"})
	// Detachment and cancellation happen before waiting for active file work.
	s.work.Lock()
	x.network.Wait()
	if x.model != nil {
		x.model.Close()
		x.model = nil
	}
	vc.Wipe(x.master[:])
	vc.Wipe(x.credential[:])
	x.store.Close()
	s.work.Unlock()
	if x.remote != nil && x.remoteSession.Token != "" {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = x.remote.Revoke(ctx, x.remoteSession)
		}()
	}
}
func (s *Service) with(fn func(*session) error) error {
	s.work.Lock()
	defer s.work.Unlock()
	s.mu.Lock()
	x := s.session
	s.mu.Unlock()
	if x == nil {
		return ErrLocked
	}
	if e := x.ctx.Err(); e != nil {
		return ErrLocked
	}
	e := fn(x)
	if x.ctx.Err() != nil {
		return ErrLocked
	}
	return e
}
func (s *Service) Select(accountPath, storePath string, copyAccount bool) error {
	s.Lock()
	a, e := local.LoadAccount(accountPath)
	if e != nil {
		return e
	}
	if copyAccount {
		dir := filepath.Join(filepath.Dir(s.configPath), a.ID.String())
		if os.MkdirAll(dir, 0700) != nil {
			return vc.ErrIO
		}
		target := filepath.Join(dir, "account.v1")
		if e = local.Publish(target, func(w io.Writer) error { _, e := w.Write(local.AccountBytes(a)); return e }); e != nil {
			return e
		}
		accountPath = target
		storePath = filepath.Join(dir, "local.db")
	}
	if storePath == "" {
		storePath = filepath.Join(filepath.Dir(accountPath), "local.db")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.AccountPath = accountPath
	s.config.StorePath = storePath
	s.config.Server = ""
	s.lastSync = ""
	return s.save()
}
func (s *Service) Create(password, recoveryPath string) error {
	if password == "" || recoveryPath == "" {
		return ErrInvalid
	}
	p := []byte(password)
	defer vc.Wipe(p)
	a, master, recovery, e := vc.NewAccount(p)
	if e != nil {
		return e
	}
	defer vc.Wipe(master[:])
	defer vc.Wipe(recovery[:])
	dir := filepath.Join(filepath.Dir(s.configPath), a.ID.String())
	if os.MkdirAll(dir, 0700) != nil {
		return vc.ErrIO
	}
	path := filepath.Join(dir, "account.v1")
	code := []byte(vc.RecoveryCode(recovery) + "\n")
	defer vc.Wipe(code)
	if e = local.Publish(recoveryPath, func(w io.Writer) error { _, e := w.Write(code); return e }); e != nil {
		return e
	}
	if e = local.Publish(path, func(w io.Writer) error { _, e := w.Write(local.AccountBytes(a)); return e }); e != nil {
		return e
	}
	return s.Select(path, "", false)
}
func (s *Service) Unlock(secret string, recovery bool) error {
	s.work.Lock()
	defer s.work.Unlock()
	s.mu.Lock()
	if s.session != nil {
		s.mu.Unlock()
		return nil
	}
	cfg := s.config
	generation := s.generation
	s.mu.Unlock()
	a, e := local.LoadAccount(cfg.AccountPath)
	if e != nil {
		return e
	}
	var master, credential vc.Key
	if recovery {
		master, e = a.Recover(secret)
	} else {
		p := []byte(secret)
		master, e = a.Unlock(p)
		if e == nil {
			var keys vc.Keys
			keys, e = vc.Derive(p, a.Descriptor)
			if e == nil {
				credential = vc.Credential(keys.Auth, a.ID)
			}
			keys.Clear()
		}
		vc.Wipe(p)
	}
	defer vc.Wipe(master[:])
	defer vc.Wipe(credential[:])
	if e != nil {
		return errors.New("UNLOCK_FAILED")
	}
	return s.startSession(context.Background(), cfg, a, master, credential, recovery, generation)
}

func (s *Service) ExportAccount(path string) error {
	return s.with(func(x *session) error {
		return local.Publish(path, func(w io.Writer) error { _, e := w.Write(local.AccountBytes(x.account)); return e })
	})
}
func Code(e error) string {
	switch {
	case e == nil:
		return ""
	case errors.Is(e, ErrLocked), errors.Is(e, context.Canceled):
		return "LOCKED"
	case errors.Is(e, local.ErrExists):
		return "EXISTS"
	case errors.Is(e, local.ErrInUse):
		return "IN_USE"
	case errors.Is(e, vs.ErrConflict):
		return "STALE"
	case errors.Is(e, vs.ErrDeletionUnsupported):
		return "DELETION_UPDATE_REQUIRED"
	case errors.Is(e, vs.ErrRelayUnsupported):
		return "RELAY_UNSUPPORTED"
	case errors.Is(e, vs.ErrRelayWaiting):
		return "RELAY_WAITING"
	case errors.Is(e, vc.ErrInvalid):
		return "INVALID_DATA"
	case errors.Is(e, ErrInvalid):
		return "INVALID_INPUT"
	}
	switch e.Error() {
	case "SEMANTIC_UNAVAILABLE", "SEMANTIC_MODEL_INVALID", "SEMANTIC_CORRECTION_CONFLICT", "SEMANTIC_DISABLED", "SEARCH_REBUILD_REQUIRED", "SEARCH_LIMIT", "SEARCH_INDEX_INVALID", "SEARCH_EXTRACTION_FAILED", "SEARCH_TOOL_UNAVAILABLE":
		return e.Error()
	case "SYNC_CONNECT_REQUIRED", "UNLOCK_FAILED", "PREVIEW_UNSUPPORTED", "IMPORT_PARTIAL", "RECOVERY_LOCAL_ONLY", "FINGERPRINT_UNAVAILABLE", "FINGERPRINT_FAILED", "FINGERPRINT_DISABLED", "FINGERPRINT_MISSING", "FINGERPRINT_PASSWORD_REQUIRED", "FINGERPRINT_KEYRING", "FINGERPRINT_BUSY", "FINGERPRINT_CANCELED":
		return e.Error()
	}
	return "OPERATION_FAILED"
}
