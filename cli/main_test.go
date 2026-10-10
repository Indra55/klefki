package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	v "klefki/core/crypto"
)

func TestCLIWorkflowAndFailureSafety(t *testing.T) {
	dir := t.TempDir()
	account := filepath.Join(dir, "account.v1")
	secret := filepath.Join(dir, "password")
	recovery := filepath.Join(dir, "recovery")
	source := filepath.Join(dir, "fixture-name.txt")
	encrypted := filepath.Join(dir, "encrypted.klefki")
	plain := filepath.Join(dir, "plain")
	os.WriteFile(secret, []byte("public synthetic password\n"), 0600)
	p := bytes.Repeat([]byte("public synthetic document\n"), 200000)
	os.WriteFile(source, p, 0600)
	var log bytes.Buffer
	invoke := func(args ...string) error { return run(args, &log) }
	if e := invoke("init", "--klefki", account, "--password-file", secret, "--recovery-out", recovery); e != nil {
		t.Fatal(e)
	}
	if e := invoke("init", "--klefki", account, "--password-file", secret, "--recovery-out", recovery); e == nil {
		t.Fatal("init overwrote account")
	}
	if e := invoke("encrypt", "--klefki", account, "--password-file", secret, "-o", encrypted, source); e != nil {
		t.Fatal(e)
	}
	blob, _ := os.ReadFile(encrypted)
	if bytes.Contains(blob, []byte("fixture-name.txt")) || bytes.Contains(blob, []byte("public synthetic document")) {
		t.Fatal("plaintext leaked")
	}
	if e := invoke("decrypt", "--klefki", account, "--password-file", secret, "-o", plain, encrypted); e != nil {
		t.Fatal(e)
	}
	out, _ := os.ReadFile(plain)
	if !bytes.Equal(out, p) {
		t.Fatal("plaintext mismatch")
	}
	if e := invoke("decrypt", "--klefki", account, "--password-file", secret, "-o", plain, encrypted); e == nil {
		t.Fatal("existing output overwritten")
	}
	recovered := filepath.Join(dir, "recovered")
	if e := invoke("decrypt", "--klefki", account, "--recovery-file", recovery, "-o", recovered, encrypted); e != nil {
		t.Fatal(e)
	}
	out, _ = os.ReadFile(recovered)
	if !bytes.Equal(out, p) {
		t.Fatal("recovery mismatch")
	}
	for _, bad := range [][]byte{blob[:len(blob)-1], append(append([]byte{}, blob...), 1)} {
		os.WriteFile(encrypted, bad, 0600)
		dest := filepath.Join(dir, "must-not-exist")
		if e := invoke("decrypt", "--klefki", account, "--password-file", secret, "-o", dest, encrypted); e == nil {
			t.Fatal("corrupt file accepted")
		}
		if _, e := os.Stat(dest); !os.IsNotExist(e) {
			t.Fatal("partial plaintext published")
		}
		matches, _ := filepath.Glob(filepath.Join(dir, ".klefki-stage-*"))
		if len(matches) != 0 {
			t.Fatal("staging plaintext leaked")
		}
	}
	os.WriteFile(encrypted, blob, 0600)
	os.WriteFile(secret, []byte("wrong synthetic password"), 0600)
	if invoke("decrypt", "--klefki", account, "--password-file", secret, "-o", filepath.Join(dir, "wrong"), encrypted) == nil {
		t.Fatal("wrong password accepted")
	}
	code, _ := os.ReadFile(recovery)
	if bytes.Contains(log.Bytes(), p[:30]) || bytes.Contains(log.Bytes(), bytes.TrimSpace(code)) || bytes.Contains(log.Bytes(), []byte("public synthetic password")) {
		t.Fatal("sensitive output")
	}
}
func TestContainerBounds(t *testing.T) {
	for _, b := range [][]byte{{}, bytes.Repeat([]byte{255}, 260)} {
		if _, e := readAccount(bytes.NewReader(b)); e == nil {
			t.Fatal("invalid account")
		}
	}
}
func FuzzAccountContainer(f *testing.F) {
	d, _ := v.NewDescriptor()
	a := v.Account{ID: v.ID{1}, Descriptor: d}
	a.PasswordWrapper, _ = v.Seal(v.Key{}, v.Context{Kind: v.PasswordMaster, Account: a.ID}, make([]byte, 32), d)
	a.RecoveryWrapper, _ = v.Seal(v.Key{}, v.Context{Kind: v.RecoveryMaster, Account: a.ID}, make([]byte, 32), nil)
	f.Add(accountBytes(a))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, b []byte) { _, _ = readAccount(bytes.NewReader(b)) })
}
func FuzzFileContainer(f *testing.F) {
	h := fileHeader{Account: v.ID{1}, File: v.ID{2}, Metadata: v.ID{3}, EncryptedMetadata: make([]byte, 74)}
	h.Wrapped, _ = v.Seal(v.Key{}, v.Context{Kind: v.FileKey, Account: h.Account, Object: h.File}, make([]byte, 32), nil)
	f.Add(h.bytes())
	f.Add([]byte{})
	f.Add(bytes.Repeat([]byte{255}, 163))
	f.Fuzz(func(t *testing.T, b []byte) { _, _ = readFileHeader(bytes.NewReader(b)) })
}
