package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	vd "klefki/core/desktop"
)

func TestRecoveryConfirmationPreservesPercentInPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "100%s-ready%n", "klefki-recovery.txt")
	message := recoveryConfirmationMessage(path)
	if runtime.GOOS == "linux" {
		message = fmt.Sprintf(message, []any{}...)
	}
	if !strings.Contains(message, path) {
		t.Fatal("confirmation does not display the literal destination")
	}
}

func TestRecoverySaveUsesConfirmedFixedFilename(t *testing.T) {
	directory := t.TempDir()
	archive := filepath.Join(directory, "streamtrace-submission.zip")
	original := []byte("synthetic unrelated archive")
	if err := os.WriteFile(archive, original, 0600); err != nil {
		t.Fatal(err)
	}
	confirmed := ""
	path, err := selectRecoveryPath(func() (string, error) { return directory, nil }, func(path string) (bool, error) {
		confirmed = path
		return true, nil
	})
	if err != nil || path != filepath.Join(directory, "klefki-recovery.txt") || confirmed != path {
		t.Fatalf("unexpected recovery destination: %q, %v", path, err)
	}
	service, err := vd.New(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if err = service.Create("synthetic recovery save password", path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() != 58 {
		t.Fatal("recovery file was not saved to the confirmed destination", err)
	}
	unchanged, err := os.ReadFile(archive)
	if err != nil || string(unchanged) != string(original) {
		t.Fatal("unrelated archive changed", err)
	}
}

func TestRecoverySaveRejectsUnsafeOrCanceledSelections(t *testing.T) {
	directory := t.TempDir()
	existing := filepath.Join(directory, "klefki-recovery.txt")
	if err := os.WriteFile(existing, []byte("existing synthetic recovery file"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, directory, code string
		chooseErr, confirmErr error
		accepted              bool
		confirmCalls          int
	}{
		{name: "directory dialog error", chooseErr: errors.New("dialog error"), code: "DIALOG_FAILED"},
		{name: "directory dialog canceled", code: "CANCELED"},
		{name: "file selected as directory", directory: existing, code: "RECOVERY_FOLDER_INVALID"},
		{name: "missing directory", directory: filepath.Join(directory, "missing"), code: "RECOVERY_FOLDER_INVALID"},
		{name: "existing recovery file", directory: directory, code: "RECOVERY_EXISTS"},
		{name: "confirmation canceled", directory: t.TempDir(), code: "CANCELED", confirmCalls: 1},
		{name: "confirmation error", directory: t.TempDir(), confirmErr: errors.New("dialog error"), code: "DIALOG_FAILED", confirmCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			path, err := selectRecoveryPath(func() (string, error) { return tc.directory, tc.chooseErr }, func(string) (bool, error) {
				calls++
				return tc.accepted, tc.confirmErr
			})
			if path != "" || err == nil || err.Error() != tc.code || calls != tc.confirmCalls {
				t.Fatalf("selection did not fail closed: path=%q err=%v confirmations=%d", path, err, calls)
			}
			if tc.confirmCalls > 0 {
				entries, err := os.ReadDir(tc.directory)
				if err != nil || len(entries) != 0 {
					t.Fatal("canceled selection wrote a file", err)
				}
			}
		})
	}
	data, err := os.ReadFile(existing)
	if err != nil || string(data) != "existing synthetic recovery file" {
		t.Fatal("existing recovery file changed", err)
	}
}
