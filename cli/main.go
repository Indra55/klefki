// Command vault provides local encryption and sync. Secrets are never CLI argument values.
package main

import (
	"crypto/subtle"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/term"
	v "vault/core/crypto"
	"vault/core/local"
)

var errUsage = errors.New("invalid arguments; run vault help")
var errExists = errors.New("destination already exists")

func main() {
	if e := run(os.Args[1:], os.Stdout); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}

type options struct{ vault, password, recovery, recoveryOut, output string }

func run(args []string, out io.Writer) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		_, e := fmt.Fprintln(out, "vault init --vault PATH --recovery-out PATH [--password-file PATH]\nvault encrypt [--vault PATH] [--password-file PATH] [-o PATH] FILE\nvault decrypt [--vault PATH] [--password-file PATH | --recovery-file PATH] -o PATH FILE\nFlags must precede FILE. Existing output files are never replaced.")
		if e != nil {
			return e
		}
		_, e = fmt.Fprintln(out, syncHelp)
		if e != nil {
			return e
		}
		_, e = fmt.Fprintln(out, backupHelp)
		if e != nil {
			return e
		}
		_, e = fmt.Fprintln(out, searchHelp)
		return e
	}
	cmd := args[0]
	if cmd == "index" || cmd == "search" {
		return runSearch(args, out)
	}

	if cmd == "backup" || cmd == "restore" || cmd == "export-plain" || cmd == "check" {
		return runBackup(args, out)
	}
	if cmd == "register" || cmd == "add" || cmd == "list" || cmd == "export" || cmd == "sync" {
		return runSync(args, out)
	}
	if cmd != "init" && cmd != "encrypt" && cmd != "decrypt" {
		return errUsage
	}
	var o options
	f := flag.NewFlagSet("vault", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	f.StringVar(&o.vault, "vault", filepath.Join(".vault", "account.v1"), "")
	f.StringVar(&o.password, "password-file", "", "")
	f.StringVar(&o.recovery, "recovery-file", "", "")
	f.StringVar(&o.recoveryOut, "recovery-out", "", "")
	f.StringVar(&o.output, "o", "", "")
	if f.Parse(args[1:]) != nil || o.vault == "" || (o.password != "" && o.recovery != "") {
		return errUsage
	}
	if cmd == "init" {
		if f.NArg() != 0 || o.recoveryOut == "" || o.recovery != "" || o.output != "" {
			return errUsage
		}
		if e := initVault(o); e != nil {
			return e
		}
		fmt.Fprintln(out, "Vault initialized. Recovery code saved to the selected private file.")
		return nil
	}
	if f.NArg() != 1 || o.recoveryOut != "" || (cmd == "decrypt" && o.output == "") || (cmd == "encrypt" && o.recovery != "") {
		return errUsage
	}
	a, e := loadAccount(o.vault)
	if e != nil {
		return e
	}
	var master v.Key
	if o.recovery != "" {
		b, e := secretFile(o.recovery, 57)
		if e != nil {
			return e
		}
		defer v.Wipe(b)
		master, e = a.Recover(string(b))
		if e != nil {
			return e
		}
	} else {
		p, e := password(o.password, false)
		if e != nil {
			return e
		}
		defer v.Wipe(p)
		master, e = a.Unlock(p)
		if e != nil {
			return e
		}
	}
	defer v.Wipe(master[:])
	if cmd == "encrypt" {
		e = encryptFile(a, master, f.Arg(0), o.output)
	} else {
		e = decryptFile(a, master, f.Arg(0), o.output)
	}
	if e != nil {
		return e
	}
	fmt.Fprintln(out, "Done.")
	return nil
}
func secretFile(path string, limit int) ([]byte, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, v.ErrIO
	}
	defer f.Close()
	s, e := f.Stat()
	if e != nil || !s.Mode().IsRegular() {
		return nil, v.ErrInvalid
	}
	b, e := io.ReadAll(io.LimitReader(f, int64(limit+3)))
	if e != nil {
		v.Wipe(b)
		return nil, v.ErrIO
	}
	n := len(b)
	if n > 0 && b[n-1] == '\n' {
		n--
		if n > 0 && b[n-1] == '\r' {
			n--
		}
	}
	if n > limit {
		v.Wipe(b)
		return nil, v.ErrInvalid
	}
	out := append([]byte{}, b[:n]...)
	v.Wipe(b)
	return out, nil
}
func password(path string, confirm bool) ([]byte, error) {
	if path != "" {
		return secretFile(path, 1024)
	}
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return nil, errors.New("password requires a terminal or --password-file")
	}
	fmt.Fprint(os.Stderr, "Password: ")
	p, e := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if e != nil {
		v.Wipe(p)
		return nil, v.ErrIO
	}
	if confirm {
		fmt.Fprint(os.Stderr, "Confirm password: ")
		q, e := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		same := constantEqual(p, q)
		v.Wipe(q)
		if e != nil || !same {
			v.Wipe(p)
			return nil, errors.New("password confirmation failed")
		}
	}
	return p, nil
}
func loadAccount(path string) (v.Account, error) {
	f, e := os.Open(path)
	if e != nil {
		return v.Account{}, v.ErrIO
	}
	defer f.Close()
	return readAccount(f)
}
func absent(path string) error {
	_, e := os.Lstat(path)
	if e == nil {
		return errExists
	}
	if !os.IsNotExist(e) {
		return v.ErrIO
	}
	return nil
}

// publish uses a hard link for atomic create-without-replacement on local Linux
// and Windows NTFS. No rename fallback is used because it can overwrite a target.
func publish(path string, fill func(io.Writer) error) error {
	e := local.Publish(path, fill)
	if errors.Is(e, local.ErrExists) {
		return errExists
	}
	return e
}
func initVault(o options) error {
	ap, e := filepath.Abs(o.vault)
	if e != nil {
		return v.ErrIO
	}
	rp, e := filepath.Abs(o.recoveryOut)
	if e != nil {
		return v.ErrIO
	}
	if ap == rp {
		return errUsage
	}
	if e = absent(ap); e != nil {
		return e
	}
	if e = absent(rp); e != nil {
		return e
	}
	p, e := password(o.password, true)
	if e != nil {
		return e
	}
	defer v.Wipe(p)
	a, master, recovery, e := v.NewAccount(p)
	if e != nil {
		return e
	}
	defer v.Wipe(master[:])
	defer v.Wipe(recovery[:])
	if os.MkdirAll(filepath.Dir(ap), 0700) != nil {
		return v.ErrIO
	}
	code := []byte(v.RecoveryCode(recovery) + "\n")
	defer v.Wipe(code)
	if e = publish(rp, func(w io.Writer) error { return writeAll(w, code) }); e != nil {
		return e
	}
	if e = publish(ap, func(w io.Writer) error { return writeAll(w, accountBytes(a)) }); e != nil {
		os.Remove(rp)
		return e
	}
	return nil
}
func writeAll(w io.Writer, b []byte) error {
	n, e := w.Write(b)
	if e != nil || n != len(b) {
		return v.ErrIO
	}
	return nil
}
func encryptFile(a v.Account, master v.Key, source, dest string) error {
	defer v.Wipe(master[:])
	f, e := os.Open(source)
	if e != nil {
		return v.ErrIO
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Size() < 0 || uint64(st.Size()) > v.MaxFileSize {
		return v.ErrInvalid
	}
	fid, e := v.RandomID()
	if e != nil {
		return e
	}
	mid, e := v.RandomID()
	if e != nil {
		return e
	}
	fk, e := v.RandomKey()
	if e != nil {
		return e
	}
	defer v.Wipe(fk[:])
	wrapped, e := v.Seal(master, v.Context{Kind: v.FileKey, Account: a.ID, Object: fid}, fk[:], nil)
	if e != nil {
		return e
	}
	m := v.Metadata{Type: "file", Name: filepath.Base(source), Tags: []string{}, FileID: fid.String(), Size: uint64(st.Size()), ChunkCount: v.ChunkCount(uint64(st.Size()))}
	meta, e := v.EncryptMetadata(master, a.ID, mid, m)
	if e != nil {
		return e
	}
	h := fileHeader{Account: a.ID, File: fid, Metadata: mid, Wrapped: wrapped, EncryptedMetadata: meta}
	if dest == "" {
		dest = filepath.Join(filepath.Dir(source), fid.String()+".vault")
	}
	return publish(dest, func(w io.Writer) error {
		if e := writeAll(w, h.bytes()); e != nil {
			return e
		}
		return v.EncryptStream(fk, a.ID, fid, m.Size, f, w)
	})
}
func decryptFile(a v.Account, master v.Key, source, dest string) error {
	defer v.Wipe(master[:])
	f, e := os.Open(source)
	if e != nil {
		return v.ErrIO
	}
	defer f.Close()
	h, e := readFileHeader(f)
	if e != nil {
		return e
	}
	if h.Account != a.ID {
		return v.ErrInvalid
	}
	m, e := v.DecryptMetadata(master, a.ID, h.Metadata, h.EncryptedMetadata)
	if e != nil {
		return e
	}
	if m.Type != "file" || m.FileID != h.File.String() {
		return v.ErrInvalid
	}
	raw, e := v.Open(master, v.Context{Kind: v.FileKey, Account: a.ID, Object: h.File}, h.Wrapped, nil)
	if e != nil {
		return e
	}
	defer v.Wipe(raw)
	var fk v.Key
	copy(fk[:], raw)
	defer v.Wipe(fk[:])
	return publish(dest, func(w io.Writer) error { return v.DecryptStream(fk, a.ID, h.File, m.Size, f, w) })
}

// Public input length is not hidden. Compare equal-length secrets in constant time.
func constantEqual(a, b []byte) bool { return subtle.ConstantTimeCompare(a, b) == 1 }
