package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func recoveryConfirmationMessage(path string) string {
	message := "Save your plaintext recovery code to this file?\n\n" + path + "\n\nKeep this file private and separate from your encrypted documents. Anyone with this code and your encrypted account and files can recover your documents."
	if runtime.GOOS == "linux" {
		// Wails passes the Linux message to GTK as a printf format string.
		message = strings.ReplaceAll(message, "%", "%%")
	}
	return message
}

func selectRecoveryPath(chooseDirectory func() (string, error), confirm func(string) (bool, error)) (string, error) {
	directory, err := chooseDirectory()
	if err != nil {
		return "", errors.New("DIALOG_FAILED")
	}
	if directory == "" {
		return "", errors.New("CANCELED")
	}
	info, err := os.Stat(directory)
	if err != nil || !info.IsDir() {
		return "", errors.New("RECOVERY_FOLDER_INVALID")
	}
	path, err := filepath.Abs(filepath.Join(directory, "klefki-recovery.txt"))
	if err != nil {
		return "", errors.New("RECOVERY_FOLDER_INVALID")
	}
	if _, err = os.Lstat(path); err == nil {
		return "", errors.New("RECOVERY_EXISTS")
	} else if !os.IsNotExist(err) {
		return "", errors.New("RECOVERY_FOLDER_INVALID")
	}
	accepted, err := confirm(path)
	if err != nil {
		return "", errors.New("DIALOG_FAILED")
	}
	if !accepted {
		return "", errors.New("CANCELED")
	}
	return path, nil
}
