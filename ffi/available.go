// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 Andrey Kolkov and GoGPU Contributors

package ffi

import (
	"github.com/go-webgpu/goffi/internal/hostlibc"
	"github.com/go-webgpu/goffi/internal/static"
)

// Available reports whether this build of goffi can load shared libraries and
// call foreign functions. The answer depends on the build mode:
//
//   - -tags goffi_static: always false, a compile-time constant. The build has
//     no //go:cgo_import_dynamic directives, so the linker emits a fully static
//     executable (no PT_INTERP, no DT_NEEDED) and there is no loader to ask.
//     LoadLibrary, GetSymbol and CallFunction return an error wrapping
//     ErrStaticBuild, and NewCallback panics. The tag is ignored on Windows
//     and Android, which never load libraries through cgo_import_dynamic, so
//     there Available stays true.
//   - -tags goffi_universal ("Profile U"): decided at run time, once, before
//     main. The binary re-execs itself through the host's dynamic loader with
//     the host libc preloaded. On a host whose loader goffi does not recognise,
//     or where that re-exec cannot be made to work, the process carries on as a
//     pure-Go program and Available is false; the FFI entry points then return
//     ErrNoHostLibc. The same binary can answer differently on two machines.
//   - Every other build (the default, goffi_musl, CGO_ENABLED=1): always true.
//     The libc is a DT_NEEDED dependency, so a process that reached main has
//     it; nothing is probed at run time.
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
// Under goffi_static the call is a constant false, so the compiler drops the
// accelerated branch and everything only it reaches.
func Available() bool {
	return !static.Enabled && !hostlibc.Missing
}
