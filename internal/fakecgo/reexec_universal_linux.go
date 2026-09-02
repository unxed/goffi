// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 Andrey Kolkov and GoGPU Contributors

//go:build !cgo && linux && !android && goffi_universal

// Portable "universal" re-exec bridge.
//
// A universal goffi binary imports every libc symbol with an empty SONAME, so
// it carries no DT_NEEDED and its PT_INTERP is stripped after linking. That
// makes it loadable by the kernel on any Linux distribution, but it also means
// nothing has mapped a libc into the process: the undefined dlopen/malloc/
// pthread_* symbols are unbound. The first thing that would touch libc is the
// malloc() at the top of x_cgo_init, which runs from rt0_go before the Go
// scheduler, before runtime.args, and before any OS thread is created.
//
// So, at the very top of x_cgo_init, we re-exec the process through the host's
// own dynamic loader with the host libc pre-loaded:
//
//	execve(<host-loader>, {<host-loader>, "--preload", <host-libc-soname>,
//	                       <self>, <original args...>}, <env>+guard)
//
// The host loader maps its libc, the global symbol scope now contains malloc,
// dlopen, pthread_*, __errno_location, and the re-executed process binds them
// from whichever libc the host actually ships (glibc's libc.so.6 or musl's
// libc.musl-<arch>.so.1). A guard variable in the environment stops the second
// launch from re-execing again.
//
// Everything here runs before libc exists, so it uses only raw syscalls
// (rawsyscall6) and mmap'd scratch memory -- never the Go heap, never libc,
// never anything that can grow the stack. String constants live in rodata and
// are copied into an mmap staging buffer with NUL terminators as needed
// (cstr), because C wants NUL-terminated strings and Go string literals are
// not. Argv and envp for the re-exec are read verbatim from /proc/self/cmdline
// and /proc/self/environ (both already NUL-delimited) and merely pointer-ified
// in place.
//
// Failure is best-effort: if no known host loader is found, or execve fails,
// we return and let startup proceed. FFI then cannot work (there is no libc),
// which is the documented limitation of running a universal binary on a system
// whose loader we do not recognise.

package fakecgo

// KNOWN ISSUE (work in progress): this bridge does not run yet on the very
// first (kernel-direct) launch. When _cgo_init is present, rt0_go SKIPS the
// runtime's own TLS setup and delegates it to _cgo_init (see runtime/asm_amd64.s:
// the "JZ needtls" is only taken when _cgo_init is nil). So at x_cgo_init time,
// on a no-interpreter binary the kernel loaded directly, the %fs/TLS base is
// not initialised. The Go compiler emits `MOVQ FS:-8, R14` (reload the g
// register) after every call to an ABI0 assembly function, and that TLS read
// faults before we reach execve.
//
// Fix in progress: a tiny per-arch asm shim (setupUniversalTLS) called as the
// first thing in x_cgo_init on the universal build, which, only when no TLS is
// set up yet (first launch), points %fs (amd64) / TPIDR_EL0 (arm64) at a
// scratch page so the g-reloads read mapped memory. On the re-executed launch
// the host loader sets up real TLS, so the shim is a no-op. The code below is
// otherwise complete and compiles; it is exercised end-to-end once the shim
// lands. See the conversation notes / docs/PROFILE_U.md (pending).

import "unsafe"

func rawsyscall6(trap, a1, a2, a3, a4, a5, a6 uintptr) (r1 uintptr)

const (
	atFDCWD = ^uintptr(99) // AT_FDCWD (-100) as unsigned
	oRDONLY = 0
	fOK     = 0
	protRW  = 0x3  // PROT_READ | PROT_WRITE
	mapPA   = 0x22 // MAP_PRIVATE | MAP_ANONYMOUS
	ptrSize = 8    // universal build is 64-bit only

	envBufCap  = 256 << 10
	cmdBufCap  = 256 << 10
	exeBufCap  = 4 << 10
	strBufCap  = 8 << 10
	ptrArrCap  = 4 << 10 // max pointers per argv/envp array (incl. nil terminator)
	ptrArrSize = ptrArrCap * ptrSize

	guardVar = "GOFFI_UNIVERSAL_REEXEC=1"
	guardKey = "GOFFI_UNIVERSAL_REEXEC="
)

// Bump allocator over an mmap staging buffer for NUL-terminated C strings.
// Set once, at the start of maybeReexecUniversal; single-threaded at that point.
var (
	strBufBase uintptr // base of mmap staging buffer (uintptr: no GC write barrier)
	strBufOff  uintptr
)

//go:nosplit
func sysErr(r uintptr) bool { return r > ^uintptr(4095) } // r in [-4095, -1]

//go:nosplit
func mmapAnon(n uintptr) unsafe.Pointer {
	r := rawsyscall6(sysMmap, 0, n, protRW, mapPA, ^uintptr(0) /* fd -1 */, 0)
	if sysErr(r) {
		return nil
	}
	return unsafe.Pointer(r)
}

// cstr copies s into the staging buffer, appends a NUL, and returns a *byte to
// the copy. Returns nil if the staging buffer is exhausted.
//
//go:nosplit
func cstr(s string) *byte {
	n := uintptr(len(s))
	if strBufBase == 0 || strBufOff+n+1 > strBufCap {
		return nil
	}
	start := strBufOff
	for i := uintptr(0); i < n; i++ {
		*(*byte)(unsafe.Add(unsafe.Pointer(strBufBase), start+i)) = s[i]
	}
	*(*byte)(unsafe.Add(unsafe.Pointer(strBufBase), start+n)) = 0
	strBufOff = start + n + 1
	return (*byte)(unsafe.Add(unsafe.Pointer(strBufBase), start))
}

//go:nosplit
func fileExists(pathC *byte) bool {
	if pathC == nil {
		return false
	}
	r := rawsyscall6(sysFaccessat, atFDCWD, uintptr(unsafe.Pointer(pathC)), fOK, 0, 0, 0)
	return r == 0
}

// readAll reads the whole file at pathC into [base, base+capBytes), returning
// the byte count, or -1 on error.
//
//go:nosplit
func readAll(pathC *byte, base unsafe.Pointer, capBytes uintptr) int {
	fd := rawsyscall6(sysOpenat, atFDCWD, uintptr(unsafe.Pointer(pathC)), oRDONLY, 0, 0, 0)
	if sysErr(fd) {
		return -1
	}
	var total uintptr
	for total < capBytes {
		n := rawsyscall6(sysRead, fd, uintptr(unsafe.Add(base, total)), capBytes-total, 0, 0, 0)
		if sysErr(n) {
			rawsyscall6(sysClose, fd, 0, 0, 0, 0, 0)
			return -1
		}
		if n == 0 {
			break
		}
		total += n
	}
	rawsyscall6(sysClose, fd, 0, 0, 0, 0, 0)
	return int(total)
}

//go:nosplit
func setPtr(base unsafe.Pointer, idx int, val uintptr) {
	*(*uintptr)(unsafe.Add(base, uintptr(idx)*ptrSize)) = val
}

// matchAt reports whether the NUL-delimited entry starting at base+off begins
// with key.
//
//go:nosplit
func matchAt(base unsafe.Pointer, off, length int, key string) bool {
	if off+len(key) > length {
		return false
	}
	for i := 0; i < len(key); i++ {
		if *(*byte)(unsafe.Add(base, off+i)) != key[i] {
			return false
		}
	}
	return true
}

// diag writes a short message to stderr (best effort). write(2) takes a
// pointer+length, so no NUL is needed and the message can live in rodata --
// this works even before the cstr staging buffer is set up.
//
//go:nosplit
func diag(msg string) {
	rawsyscall6(sysWrite, 2, uintptr(unsafe.Pointer(unsafe.StringData(msg))), uintptr(len(msg)), 0, 0, 0)
}

// maybeReexecUniversal is invoked at the very top of x_cgo_init. On the first
// launch of a universal binary it re-execs through the host loader with libc
// pre-loaded; on the re-executed launch (guard present) it returns immediately.
//
//go:nosplit
func maybeReexecUniversal() {
	// Staging buffer for C strings first: everything below needs cstr().
	sb := mmapAnon(strBufCap)
	if sb == nil {
		return
	}
	strBufBase = uintptr(sb)
	strBufOff = 0

	envBase := mmapAnon(envBufCap)
	if envBase == nil {
		return
	}
	envLen := readAll(cstr("/proc/self/environ"), envBase, envBufCap)
	if envLen < 0 {
		return
	}

	// Guard: if we already re-executed, do nothing.
	for off := 0; off < envLen; {
		if matchAt(envBase, off, envLen, guardKey) {
			return
		}
		for off < envLen && *(*byte)(unsafe.Add(envBase, off)) != 0 {
			off++
		}
		off++ // skip NUL
	}

	// Pick the host loader + libc SONAME by probing known loader paths.
	// Prefer glibc when both are present; fall back to musl.
	var loaderC, sonameC *byte
	if g := cstr(glibcLoader); fileExists(g) {
		loaderC = g
		sonameC = cstr(glibcLibc)
	} else if m := cstr(muslLoader); fileExists(m) {
		loaderC = m
		sonameC = cstr(muslLibc)
	}
	if loaderC != nil {
	}
	if loaderC == nil || sonameC == nil {
		diag("goffi: universal build: no known host dynamic loader found; FFI unavailable\n")
		return
	}

	// Resolve our own executable path for the loader to run.
	exeBase := mmapAnon(exeBufCap)
	var exeC *byte
	if exeBase != nil {
		n := rawsyscall6(sysReadlinkat, atFDCWD,
			uintptr(unsafe.Pointer(cstr("/proc/self/exe"))),
			uintptr(exeBase), exeBufCap-1, 0, 0)
		if !sysErr(n) && n != 0 {
			*(*byte)(unsafe.Add(exeBase, n)) = 0
			exeC = (*byte)(exeBase)
		}
	}
	if exeC == nil {
		exeC = cstr("/proc/self/exe")
	}

	// Read original argv from /proc/self/cmdline (NUL-delimited).
	cmdBase := mmapAnon(cmdBufCap)
	if cmdBase == nil {
		return
	}
	cmdLen := readAll(cstr("/proc/self/cmdline"), cmdBase, cmdBufCap)
	if cmdLen < 0 {
		return
	}

	// Build argv: {loader, "--preload", soname, self, <original argv[1:]...>, NULL}
	argvBase := mmapAnon(ptrArrSize)
	envpBase := mmapAnon(ptrArrSize)
	if argvBase == nil || envpBase == nil {
		return
	}
	preloadC := cstr("--preload")
	if preloadC == nil {
		return
	}
	ai := 0
	setPtr(argvBase, ai, uintptr(unsafe.Pointer(loaderC)))
	ai++
	setPtr(argvBase, ai, uintptr(unsafe.Pointer(preloadC)))
	ai++
	setPtr(argvBase, ai, uintptr(unsafe.Pointer(sonameC)))
	ai++
	setPtr(argvBase, ai, uintptr(unsafe.Pointer(exeC)))
	ai++
	// Append original args, skipping argv[0] (the program name) since exeC
	// already occupies argv[0] of the re-executed program.
	{
		off := 0
		// skip argv[0]
		for off < cmdLen && *(*byte)(unsafe.Add(cmdBase, off)) != 0 {
			off++
		}
		off++ // skip its NUL
		for off < cmdLen && ai < ptrArrCap-1 {
			setPtr(argvBase, ai, uintptr(unsafe.Add(cmdBase, off)))
			ai++
			for off < cmdLen && *(*byte)(unsafe.Add(cmdBase, off)) != 0 {
				off++
			}
			off++ // skip NUL
		}
	}
	setPtr(argvBase, ai, 0) // NULL-terminate argv

	// Build envp: <existing environ...> + guard, NULL-terminated.
	ei := 0
	for off := 0; off < envLen && ei < ptrArrCap-2; {
		setPtr(envpBase, ei, uintptr(unsafe.Add(envBase, off)))
		ei++
		for off < envLen && *(*byte)(unsafe.Add(envBase, off)) != 0 {
			off++
		}
		off++ // skip NUL
	}
	if g := cstr(guardVar); g != nil {
		setPtr(envpBase, ei, uintptr(unsafe.Pointer(g)))
		ei++
	}
	setPtr(envpBase, ei, 0) // NULL-terminate envp

	rawsyscall6(sysExecve,
		uintptr(unsafe.Pointer(loaderC)),
		uintptr(argvBase),
		uintptr(envpBase), 0, 0, 0)

	// Only reached if execve failed.
	diag("goffi: universal build: re-exec through host loader failed; FFI unavailable\n")
}
