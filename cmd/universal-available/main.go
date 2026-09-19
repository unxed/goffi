// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 Andrey Kolkov and GoGPU Contributors

// Command universal-available prints whether the process has a libc to call
// into, and exits 0 either way. It is the test subject for hosts where the
// re-exec through the host loader cannot be trusted to work -- a library
// preloaded through /etc/ld.so.preload that aborts while it initialises -- and
// where a universal binary is expected to carry on without FFI instead of
// dying before main.
package main

import (
	"fmt"

	"github.com/go-webgpu/goffi/ffi"
)

func main() {
	fmt.Printf("ffi.Available = %v\n", ffi.Available())
}
