// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 Andrey Kolkov and GoGPU Contributors

package ffi

import "github.com/go-webgpu/goffi/internal/static"

// Available reports whether this build of goffi can load shared libraries and
// call foreign functions.
//
// It returns false only when the binary was built with -tags goffi_static, a
// mode that strips every //go:cgo_import_dynamic directive so the Go linker
// produces a fully static executable (no PT_INTERP, no DT_NEEDED). In that mode
// LoadLibrary, GetSymbol and CallFunction return an error wrapping
// ErrStaticBuild instead of calling into libc.
//
// Callers that have a pure-Go fallback should branch on this at startup rather
// than treating the first LoadLibrary failure as fatal:
//
//	if ffi.Available() {
//	    backend = newAcceleratedBackend()
//	} else {
//	    backend = newPureGoBackend()
//	}
//
// The goffi_static tag is ignored on Windows and Android, where library loading
// never went through cgo_import_dynamic, so Available reports true there even
// when the tag is set.
func Available() bool {
	return !static.Enabled
}
