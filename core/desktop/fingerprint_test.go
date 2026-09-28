package desktop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	vc "vault/core/crypto"
)

type fakeFingerprint struct {
	mu          sync.Mutex
	data        []byte
	verifyError error
	loads       int
	block       chan struct{}
}

func (f *fakeFingerprint) Available(context.Context) error { return nil }
func (f *fakeFingerprint) Verify(ctx context.Context, notify func(string)) error {
	notify("touch")
	if f.block != nil {
		close(f.block)
		<-ctx.Done()
		return ctx.Err()
	}
	return f.verifyError
}
func (f *fakeFingerprint) Store(_ context.Context, _ string, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.data = append([]byte{}, data...)
	return nil
}
func (f *fakeFingerprint) Load(_ context.Context, _ string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loads++
	if f.data == nil {
		return nil, ErrFingerprintMissing
	}
	return append([]byte{}, f.data...), nil
}
func (f *fakeFingerprint) Delete(context.Context, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	vc.Wipe(f.data)
	f.data = nil
	return nil
}
func TestFingerprintEnableUnlockAndDisable(t *testing.T) {
	s, _ := testService(t)
	provider := &fakeFingerprint{}
	s.SetFingerprintProvider(provider)
	rev := put(t, s, "fingerprint-test.txt", nil)
	if e := s.EnableFingerprint(); e != nil {
		t.Fatal(e)
	}
	if !s.State().Fingerprint.Enabled {
		t.Fatal("not enabled")
	}
	s.Lock()
	if e := s.UnlockFingerprint(); e != nil {
		t.Fatal(e)
	}
	if s.State().RecoveryOnly {
		t.Fatal("fingerprint lost sync credential")
	}
	if find(t, s, rev).Name != "fingerprint-test.txt" {
		t.Fatal("document missing")
	}
	if e := s.DisableFingerprint(); e != nil {
		t.Fatal(e)
	}
	s.Lock()
	if e := s.UnlockFingerprint(); e != ErrFingerprintDisabled {
		t.Fatal("disabled access", e)
	}
	if e := s.Unlock("synthetic desktop password", false); e != nil {
		t.Fatal("password fallback", e)
	}
}
func TestFingerprintFailDoesNotReadKeys(t *testing.T) {
	s, _ := testService(t)
	f := &fakeFingerprint{}
	s.SetFingerprintProvider(f)
	if e := s.EnableFingerprint(); e != nil {
		t.Fatal(e)
	}
	s.Lock()
	f.verifyError = ErrFingerprintFailed
	if e := s.UnlockFingerprint(); e != ErrFingerprintFailed {
		t.Fatal(e)
	}
	if f.loads != 0 || s.State().Unlocked {
		t.Fatal("failure released keys")
	}
	f.verifyError = nil
	f.data[0] ^= 1
	if e := s.UnlockFingerprint(); e == nil {
		t.Fatal("invalid keyring record accepted")
	}
	if s.State().Unlocked {
		t.Fatal("invalid record unlocked")
	}
}
func TestFingerprintCancelAndLock(t *testing.T) {
	s, _ := testService(t)
	f := &fakeFingerprint{}
	s.SetFingerprintProvider(f)
	if e := s.EnableFingerprint(); e != nil {
		t.Fatal(e)
	}
	s.Lock()
	f.block = make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- s.UnlockFingerprint() }()
	<-f.block
	s.Lock()
	select {
	case e := <-done:
		if !errors.Is(e, context.Canceled) && e != ErrLocked && e != ErrFingerprintCanceled {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("lock did not cancel scan")
	}
	if f.loads != 0 || s.State().Unlocked {
		t.Fatal("canceled scan released keys")
	}
}
func TestFingerprintRecoveryCannotEnable(t *testing.T) {
	s, code := testService(t)
	s.SetFingerprintProvider(&fakeFingerprint{})
	s.Lock()
	if e := s.Unlock(readRecovery(t, code), true); e != nil {
		t.Fatal(e)
	}
	if e := s.EnableFingerprint(); e != ErrFingerprintPassword {
		t.Fatal("recovery enabled fingerprint", e)
	}
}

func readRecovery(t *testing.T, path string) string {
	t.Helper()
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	return strings.TrimSpace(string(b))
}

func TestFingerprintAcrossRestart(t *testing.T) {
	s, _ := testService(t)
	p := &fakeFingerprint{}
	s.SetFingerprintProvider(p)
	if e := s.EnableFingerprint(); e != nil {
		t.Fatal(e)
	}
	root := filepath.Dir(s.configPath)
	s.Close()
	again, e := New(root, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer again.Close()
	again.SetFingerprintProvider(p)
	if e = again.UnlockFingerprint(); e != nil {
		t.Fatal(e)
	}
	if !again.State().Unlocked {
		t.Fatal("did not unlock after restart")
	}
}
