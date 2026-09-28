//go:build !linux

package main

import vd "vault/core/desktop"

func newFingerprintProvider() vd.FingerprintProvider { return nil }
