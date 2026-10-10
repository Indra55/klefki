//go:build linux

package main

import (
	"context"
	"errors"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	vd "klefki/core/desktop"
)

type retryFingerprintDevice struct {
	dbus.BusObject
	calls []string
	fail  string
}

func (d *retryFingerprintDevice) Path() dbus.ObjectPath {
	return "/net/reactivated/Fprint/Device/0"
}
func (d *retryFingerprintDevice) CallWithContext(ctx context.Context, method string, flags dbus.Flags, args ...interface{}) *dbus.Call {
	d.calls = append(d.calls, method)
	if e := ctx.Err(); e != nil {
		return &dbus.Call{Err: e}
	}
	if method == d.fail {
		return &dbus.Call{Err: errors.New("reader unavailable")}
	}
	if method == fprintInterface+".VerifyStart" && (len(args) != 1 || args[0] != "any") {
		return &dbus.Call{Err: errors.New("invalid finger selection")}
	}
	return &dbus.Call{}
}

func TestFingerprintVerificationAttempts(t *testing.T) {
	for _, test := range []struct {
		name     string
		statuses []string
		restarts int
		fail     string
		want     error
	}{
		{name: "first match", statuses: []string{"verify-match"}},
		{name: "second match", statuses: []string{"verify-no-match", "verify-match"}, restarts: 1},
		{name: "third match", statuses: []string{"verify-no-match", "verify-no-match", "verify-match"}, restarts: 2},
		{name: "third mismatch stops before later match", statuses: []string{"verify-no-match", "verify-no-match", "verify-no-match", "verify-match"}, restarts: 2, want: vd.ErrFingerprintFailed},
		{name: "poor contact does not consume attempts", statuses: []string{"verify-retry-scan", "verify-no-match", "verify-swipe-too-short", "verify-finger-not-centered", "verify-no-match", "verify-remove-and-retry", "verify-too-fast", "verify-match"}, restarts: 2},
		{name: "disconnected reader stops", statuses: []string{"verify-disconnected", "verify-match"}, want: vd.ErrFingerprintFailed},
		{name: "stop failure", statuses: []string{"verify-no-match", "verify-match"}, fail: fprintInterface + ".VerifyStop", want: vd.ErrFingerprintUnavailable},
		{name: "restart failure", statuses: []string{"verify-no-match", "verify-match"}, fail: fprintInterface + ".VerifyStart", want: vd.ErrFingerprintUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			device := &retryFingerprintDevice{fail: test.fail}
			signals := make(chan *dbus.Signal, len(test.statuses))
			for _, status := range test.statuses {
				done := status == "verify-match" || status == "verify-no-match" || status == "verify-disconnected"
				signals <- &dbus.Signal{Sender: ":1.42", Path: device.Path(), Name: fprintInterface + ".VerifyStatus", Body: []interface{}{status, done}}
			}
			close(signals)
			var notifications []string
			e := waitFingerprintVerification(context.Background(), device, signals, ":1.42", func(status string) { notifications = append(notifications, status) })
			if !errors.Is(e, test.want) {
				t.Fatalf("verification = %v, want %v", e, test.want)
			}
			var wantCalls []string
			for range test.restarts {
				wantCalls = append(wantCalls, fprintInterface+".VerifyStop", fprintInterface+".VerifyStart")
			}
			if test.fail != "" {
				wantCalls = append(wantCalls, fprintInterface+".VerifyStop")
				if test.fail == fprintInterface+".VerifyStart" {
					wantCalls = append(wantCalls, test.fail)
				}
			}
			if !slices.Equal(device.calls, wantCalls) {
				t.Fatalf("reader calls = %v, want %v", device.calls, wantCalls)
			}
			if test.restarts > 0 && !slices.Contains(notifications, "retry") {
				t.Fatal("missing retry feedback")
			}
		})
	}
}

func TestFingerprintVerificationCancelRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	device := &retryFingerprintDevice{}
	signals := make(chan *dbus.Signal, 1)
	signals <- &dbus.Signal{Sender: ":1.42", Path: device.Path(), Name: fprintInterface + ".VerifyStatus", Body: []interface{}{"verify-no-match", true}}
	close(signals)
	e := waitFingerprintVerification(ctx, device, signals, ":1.42", func(string) { cancel() })
	if !errors.Is(e, context.Canceled) {
		t.Fatalf("canceled verification = %v", e)
	}
	if !slices.Equal(device.calls, []string{fprintInterface + ".VerifyStop"}) {
		t.Fatalf("restarted after cancellation: %v", device.calls)
	}
}

func TestFingerprintPresenceSignal(t *testing.T) {
	for _, test := range []struct {
		name    string
		body    []interface{}
		present bool
		valid   bool
	}{
		{"contact", []interface{}{fprintInterface, map[string]dbus.Variant{"finger-present": dbus.MakeVariant(true)}, []string{}}, true, true},
		{"removed", []interface{}{fprintInterface, map[string]dbus.Variant{"finger-present": dbus.MakeVariant(false)}, []string{}}, false, true},
		{"waiting", []interface{}{fprintInterface, map[string]dbus.Variant{"finger-needed": dbus.MakeVariant(true)}, []string{}}, false, false},
		{"wrong interface", []interface{}{propertiesInterface, map[string]dbus.Variant{"finger-present": dbus.MakeVariant(true)}, []string{}}, false, false},
		{"invalid value", []interface{}{fprintInterface, map[string]dbus.Variant{"finger-present": dbus.MakeVariant("true")}, []string{}}, false, false},
		{"invalidated", []interface{}{fprintInterface, map[string]dbus.Variant{}, []string{"finger-present"}}, false, true},
		{"missing body", nil, false, false},
		{"invalid properties", []interface{}{fprintInterface, true, []string{}}, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			present, valid := fingerPresence(test.body)
			if present != test.present || valid != test.valid {
				t.Fatalf("presence = (%v, %v), want (%v, %v)", present, valid, test.present, test.valid)
			}
		})
	}
}

func TestFingerprintSignalShape(t *testing.T) {
	for _, test := range []struct {
		body  []interface{}
		valid bool
	}{{[]interface{}{"verify-match", true}, true}, {[]interface{}{"verify-no-match", true}, true}, {[]interface{}{"verify-retry-scan", false}, true}, {[]interface{}{"verify-match"}, false}, {[]interface{}{"verify-match", "true"}, false}, {[]interface{}{true, true}, false}} {
		_, _, valid := verificationStatus(test.body)
		if valid != test.valid {
			t.Fatal("signal validation")
		}
	}
}

func TestFingerprintAvailableOnHost(t *testing.T) {
	if os.Getenv("KLEFKI_FINGERPRINT_HOST_TEST") != "1" {
		t.Skip("explicit host service check")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if e := newFingerprintProvider().Available(ctx); e != nil {
		t.Fatal(e)
	}
}
