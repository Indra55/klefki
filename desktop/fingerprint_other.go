//go:build !linux

package main

import vd "klefki/core/desktop"

func newFingerprintProvider() vd.FingerprintProvider { return nil }
