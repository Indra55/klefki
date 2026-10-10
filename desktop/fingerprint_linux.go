//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"

	vc "klefki/core/crypto"
	vd "klefki/core/desktop"
)

const fprintName = "net.reactivated.Fprint"
const fprintInterface = "net.reactivated.Fprint.Device"
const propertiesInterface = "org.freedesktop.DBus.Properties"

type linuxFingerprint struct{ tool string }

func newFingerprintProvider() vd.FingerprintProvider {
	path, _ := exec.LookPath("secret-tool")
	return &linuxFingerprint{tool: path}
}
func fingerprintDevice(ctx context.Context) (*dbus.Conn, dbus.BusObject, error) {
	conn, e := dbus.ConnectSystemBus()
	if e != nil {
		return nil, nil, vd.ErrFingerprintUnavailable
	}
	var path dbus.ObjectPath
	e = conn.Object(fprintName, "/net/reactivated/Fprint/Manager").CallWithContext(ctx, "net.reactivated.Fprint.Manager.GetDefaultDevice", 0).Store(&path)
	if e != nil || !path.IsValid() {
		conn.Close()
		return nil, nil, vd.ErrFingerprintUnavailable
	}
	return conn, conn.Object(fprintName, path), nil
}
func (p *linuxFingerprint) Available(ctx context.Context) error {
	if p.tool == "" {
		return vd.ErrFingerprintKeyring
	}
	conn, device, e := fingerprintDevice(ctx)
	if e != nil {
		return e
	}
	defer conn.Close()
	var fingers []string
	if e = device.CallWithContext(ctx, fprintInterface+".ListEnrolledFingers", 0, "").Store(&fingers); e != nil || len(fingers) == 0 {
		return vd.ErrFingerprintUnavailable
	}
	secrets, e := dbus.ConnectSessionBus()
	if e != nil {
		return vd.ErrFingerprintKeyring
	}
	defer secrets.Close()
	var collection dbus.ObjectPath
	e = secrets.Object("org.freedesktop.secrets", "/org/freedesktop/secrets").CallWithContext(ctx, "org.freedesktop.Secret.Service.ReadAlias", 0, "default").Store(&collection)
	if e != nil || collection == "/" || !collection.IsValid() {
		return vd.ErrFingerprintKeyring
	}
	return nil
}
func (p *linuxFingerprint) Verify(ctx context.Context, notify func(string)) error {
	conn, device, e := fingerprintDevice(ctx)
	if e != nil {
		return e
	}
	defer conn.Close()
	if e = device.CallWithContext(ctx, fprintInterface+".Claim", 0, "").Err; e != nil {
		if de, ok := e.(dbus.Error); ok && strings.HasSuffix(de.Name, "AlreadyInUse") {
			return vd.ErrFingerprintBusy
		}
		return vd.ErrFingerprintUnavailable
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = device.CallWithContext(cleanup, fprintInterface+".VerifyStop", 0).Err
		_ = device.CallWithContext(cleanup, fprintInterface+".Release", 0).Err
	}()
	signals := make(chan *dbus.Signal, 16)
	conn.Signal(signals)
	defer conn.RemoveSignal(signals)
	var owner string
	if e = conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetNameOwner", 0, fprintName).Store(&owner); e != nil {
		return vd.ErrFingerprintUnavailable
	}
	if e = conn.AddMatchSignal(dbus.WithMatchSender(fprintName), dbus.WithMatchObjectPath(device.Path()), dbus.WithMatchInterface(fprintInterface)); e != nil {
		return vd.ErrFingerprintUnavailable
	}
	if e = conn.AddMatchSignal(dbus.WithMatchSender(fprintName), dbus.WithMatchObjectPath(device.Path()), dbus.WithMatchInterface(propertiesInterface), dbus.WithMatchMember("PropertiesChanged")); e != nil {
		return vd.ErrFingerprintUnavailable
	}
	if e = device.CallWithContext(ctx, fprintInterface+".VerifyStart", 0, "any").Err; e != nil {
		return vd.ErrFingerprintUnavailable
	}
	notify("touch")
	var present dbus.Variant
	if e = device.CallWithContext(ctx, propertiesInterface+".Get", 0, fprintInterface, "finger-present").Store(&present); e == nil && present.Value() == true {
		notify("scanning")
	}
	return waitFingerprintVerification(ctx, device, signals, owner, notify)
}
func waitFingerprintVerification(ctx context.Context, device dbus.BusObject, signals <-chan *dbus.Signal, owner string, notify func(string)) error {
	failed := 0
	for {
		if e := ctx.Err(); e != nil {
			return e
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case sig, ok := <-signals:
			if !ok {
				return vd.ErrFingerprintUnavailable
			}
			if sig == nil || sig.Sender != owner || sig.Path != device.Path() {
				continue
			}
			if sig.Name == propertiesInterface+".PropertiesChanged" {
				if present, valid := fingerPresence(sig.Body); valid {
					if present {
						notify("scanning")
					} else {
						notify("touch")
					}
				}
				continue
			}
			if sig.Name != fprintInterface+".VerifyStatus" {
				continue
			}
			status, done, valid := verificationStatus(sig.Body)
			if !valid {
				return vd.ErrFingerprintFailed
			}
			if status == "verify-match" && done {
				return nil
			}
			if status == "verify-no-match" && done {
				failed++
				if failed >= 3 {
					return vd.ErrFingerprintFailed
				}
				notify("retry")
				if e := device.CallWithContext(ctx, fprintInterface+".VerifyStop", 0).Err; e != nil {
					if ctx.Err() != nil {
						return ctx.Err()
					}
					return vd.ErrFingerprintUnavailable
				}
				if e := device.CallWithContext(ctx, fprintInterface+".VerifyStart", 0, "any").Err; e != nil {
					if ctx.Err() != nil {
						return ctx.Err()
					}
					return vd.ErrFingerprintUnavailable
				}
				continue
			}
			if done {
				return vd.ErrFingerprintFailed
			}
			switch status {
			case "verify-retry-scan", "verify-swipe-too-short", "verify-finger-not-centered", "verify-remove-and-retry", "verify-too-fast":
				notify("retry")
			default:
				return vd.ErrFingerprintFailed
			}
		}
	}
}
func fingerPresence(body []interface{}) (bool, bool) {
	if len(body) != 3 || body[0] != fprintInterface {
		return false, false
	}
	changed, ok := body[1].(map[string]dbus.Variant)
	invalidated, valid := body[2].([]string)
	if !ok || !valid {
		return false, false
	}
	if _, ok := changed["finger-present"]; !ok && slices.Contains(invalidated, "finger-present") {
		return false, true
	}
	present, ok := changed["finger-present"].Value().(bool)
	return present, ok
}
func verificationStatus(body []interface{}) (string, bool, bool) {
	if len(body) != 2 {
		return "", false, false
	}
	status, ok := body[0].(string)
	done, okDone := body[1].(bool)
	return status, done, ok && okDone
}

// Secret material travels through stdin/stdout, never process arguments or logs.
func (p *linuxFingerprint) Store(ctx context.Context, binding string, data []byte) error {
	encoded := make([]byte, base64.StdEncoding.EncodedLen(len(data)))
	base64.StdEncoding.Encode(encoded, data)
	defer vc.Wipe(encoded)
	cmd := exec.CommandContext(ctx, p.tool, "store", "--collection=default", "--label=Klefki fingerprint unlock", "application", "klefki-desktop", "account-binding", binding)
	cmd.Stdin = bytes.NewReader(encoded)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	if e := cmd.Run(); e != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return vd.ErrFingerprintKeyring
	}
	return nil
}

type boundedSecret struct{ data []byte }

func (b *boundedSecret) Write(p []byte) (int, error) {
	if len(b.data)+len(p) > 4096 {
		return 0, vd.ErrFingerprintMissing
	}
	b.data = append(b.data, p...)
	return len(p), nil
}
func (p *linuxFingerprint) Load(ctx context.Context, binding string) ([]byte, error) {
	out := &boundedSecret{}
	defer vc.Wipe(out.data)
	cmd := exec.CommandContext(ctx, p.tool, "lookup", "application", "klefki-desktop", "account-binding", binding)
	cmd.Stdout = out
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	if e := cmd.Run(); e != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, vd.ErrFingerprintMissing
	}
	if len(out.data) == 0 {
		return nil, vd.ErrFingerprintMissing
	}
	encoded := bytes.TrimSpace(out.data)
	data := make([]byte, base64.StdEncoding.DecodedLen(len(encoded)))
	n, e := base64.StdEncoding.Strict().Decode(data, encoded)
	if e != nil {
		vc.Wipe(data)
		return nil, vd.ErrFingerprintMissing
	}
	return data[:n], nil
}
func (p *linuxFingerprint) Delete(ctx context.Context, binding string) error {
	cmd := exec.CommandContext(ctx, p.tool, "clear", "application", "klefki-desktop", "account-binding", binding)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	if e := cmd.Run(); e != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return vd.ErrFingerprintKeyring
	}
	return nil
}
