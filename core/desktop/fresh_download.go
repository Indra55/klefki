package desktop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	vc "klefki/core/crypto"
	vs "klefki/core/sync"
	"klefki/core/syncwire"
)

// A failed attempt resumes the same encrypted store and enrolled device.
type FreshDownloadState struct {
	Source string `json:"source"`
	Server string `json:"server"`
	Path   string `json:"path"`
}

// FreshDownload stages and authenticates a separate copy before changing the
// selected store. The original database and encrypted account file remain intact.
func (s *Service) FreshDownload() error { return s.freshDownload(nil) }

func (s *Service) freshDownload(remote *vs.Remote) error {
	s.work.Lock()
	defer s.work.Unlock()
	s.mu.Lock()
	x := s.session
	s.mu.Unlock()
	if x == nil || x.ctx.Err() != nil {
		return ErrLocked
	}
	return func() (result error) {
		if x.recovery {
			return errors.New("RECOVERY_LOCAL_ONLY")
		}
		s.mu.Lock()
		cfg := s.config
		if cfg.Server == "" {
			s.mu.Unlock()
			return errors.New("SYNC_CONNECT_REQUIRED")
		}
		if s.syncRunning {
			s.mu.Unlock()
			return errors.New("SYNC_DOWNLOAD_BUSY")
		}
		s.syncRunning, s.syncOwner = true, x
		s.syncInfo.Running, s.syncInfo.Phase = true, "checking"
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			if s.syncOwner == x {
				s.syncRunning, s.syncAgain, s.syncRefresh = false, false, false
			}
			if s.session == x {
				s.syncInfo.Running = false
				s.syncInfo.Phase, s.syncState = syncFailure(result)
			}
			s.mu.Unlock()
			s.emitSync(x)
		}()
		if n, err := x.store.Pending(); err != nil {
			return err
		} else if n != 0 {
			return errors.New("SYNC_DOWNLOAD_PENDING")
		}
		known, err := x.store.Check(x.master)
		if err != nil {
			return err
		}
		if remote == nil {
			remote, err = vs.NewRemote(cfg.Server, nil, false)
			if err != nil {
				return err
			}
		}
		// Never recreate an absent server account or switch its storage mode here.
		auth, err := remote.Login(x.ctx, x.account.ID, x.credential)
		if err != nil {
			return err
		}
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = remote.Revoke(ctx, auth)
		}()
		stage := cfg.FreshDownload
		if stage != nil && (stage.Source != cfg.StorePath || stage.Server != cfg.Server) {
			return errors.New("SYNC_DOWNLOAD_SOURCE_CHANGED")
		}
		if stage == nil {
			dir, err := os.MkdirTemp(filepath.Dir(cfg.StorePath), ".klefki-download-*")
			if err != nil {
				return vc.ErrIO
			}
			stage = &FreshDownloadState{Source: cfg.StorePath, Server: cfg.Server, Path: filepath.Join(dir, "local.db")}
			s.mu.Lock()
			if s.session != x || x.ctx.Err() != nil {
				s.mu.Unlock()
				os.Remove(dir)
				return ErrLocked
			}
			s.config.FreshDownload = stage
			err = s.save()
			if err != nil {
				s.config.FreshDownload = nil
			}
			s.mu.Unlock()
			if err != nil {
				os.Remove(dir)
				return err
			}
		}
		copy, err := vs.Open(stage.Path, x.account.ID)
		if err != nil {
			return err
		}
		defer copy.Close()
		policy, err := x.store.TrashPolicy()
		if err != nil {
			return err
		}
		if err = copy.SetTrashOverride(policy.Override); err != nil {
			return err
		}
		client := vs.Client{Store: copy, Remote: remote, Session: auth, Master: x.master, Observe: func(p syncwire.Progress) { s.observeSync(x, p) }}
		defer vc.Wipe(client.Master[:])
		if err = client.Sync(x.ctx); err != nil {
			return err
		}
		revisions, err := copy.Check(x.master)
		if err != nil {
			return err
		}
		present := make(map[string]bool, len(revisions))
		for _, revision := range revisions {
			present[revision.Revision] = true
		}
		for _, revision := range known {
			if present[revision.Revision] {
				continue
			}
			deleted, err := copy.Deleted(revision.Object)
			if err != nil {
				return err
			}
			if !deleted {
				return errors.New("SYNC_DOWNLOAD_INCOMPLETE")
			}
		}
		if err = copy.Close(); err != nil {
			return err
		}
		s.mu.Lock()
		if s.session != x || x.ctx.Err() != nil {
			s.mu.Unlock()
			return ErrLocked
		}
		previous := s.config
		s.config.StorePath, s.config.PreviousStorePath, s.config.FreshDownload = stage.Path, cfg.StorePath, nil
		if err = s.save(); err != nil {
			s.config = previous
			s.mu.Unlock()
			return err
		}
		// Detach while holding both locks: no edits can reach the old store after
		// the configured path changes. Unlock reopens only the verified copy.
		s.session = nil
		s.generation++
		generation := s.generation
		x.cancel()
		s.pending, s.lastSync, s.syncState = 0, "", "Locked"
		s.syncInfo = SyncInfo{Phase: "locked"}
		s.syncPanel, s.syncStatusRequested = false, false
		if s.syncTimer != nil {
			s.syncTimer.Stop()
			s.syncTimer = nil
		}
		s.mu.Unlock()
		s.event(Event{Generation: generation, Kind: "locked"})
		x.network.Wait()
		if x.remote != nil && x.remoteSession.Token != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = x.remote.Revoke(ctx, x.remoteSession)
			cancel()
		}
		if x.model != nil {
			x.model.Close()
			x.model = nil
		}
		vc.Wipe(x.master[:])
		vc.Wipe(x.credential[:])
		x.store.Close()
		return nil
	}()
}
