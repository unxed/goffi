# Profile U — one CGO-free binary for both glibc and musl

The universal ("Profile U") build produces a **single** `CGO_ENABLED=0` binary
that does live, in-process FFI on **both** glibc and musl systems (Debian,
Ubuntu, Alpine, …) — no C toolchain, no per-distro rebuild.

## Build

```sh
bash scripts/build-universal.sh -o app ./path/to/your/main
go run ./cmd/goffi-audit app      # asserts: no PT_INTERP, no DT_NEEDED
```

`scripts/build-universal.sh` builds with `-tags goffi_universal CGO_ENABLED=0`
and then strips the program interpreter. The universal build needs **no**
`-gcflags` (unlike the `goffi_musl` build).

## How it works

1. **No `DT_NEEDED`.** Every libc symbol is imported with an empty SONAME, so
   the linker records undefined symbols but pins the binary to no specific libc.
2. **No `PT_INTERP`.** The interpreter is stripped after linking, so the kernel
   loads the binary directly on any distribution (as it does a static binary).
3. **Re-exec through the host loader.** At the very top of startup — before any
   libc symbol is touched — the process re-execs itself through the host's own
   dynamic loader with the host libc pre-loaded. musl's loader binds the
   empty-SONAME symbols of a no-interp binary directly; glibc's loader only
   binds a main object that carries a `PT_INTERP`, so on glibc the binary hands
   the loader an in-memory copy of itself with the interpreter header restored.
   Either way the symbols bind against whichever libc the host ships.

The host loader and libc are discovered from an auditable table
(`internal/loader`), also exposed publicly:

```go
ffi.HostLoader() // e.g. "/lib64/ld-linux-x86-64.so.2" or "/lib/ld-musl-x86_64.so.1"
ffi.HostLibC()   // e.g. "libc.so.6" or "libc.musl-x86_64.so.1"
ffi.LibcKind()   // "glibc" | "musl" | "unknown"
```

## Limitations

- **Linux only.** amd64 is run-tested; arm64 is cross-compile-verified.
- A host whose dynamic loader goffi does not recognise cannot do FFI (the
  process cannot bind a libc); that is a hard limitation of universal mode.
- After the re-exec, `argv[0]` becomes the resolved executable path.
- The universal build **owns the cgo runtime**; do not combine it with purego's
  fakecgo. To run goffi alongside purego, use the default build with
  `-tags nofakecgo` (see `docs/MUSL.md` and the CI `purego-coexistence` job).

## Attribution

The Profile U concept — an auditable "no `PT_INTERP`, no `DT_NEEDED`, reach the
host libc through its own loader" contract — is ported from
[`unxed/static-everywhere`](https://github.com/unxed/static-everywhere). The
in-process foreign-libc loader (`pg83/solo`) is intentionally **not** vendored;
goffi only needs the host's own libc, so it reuses the host loader via
`--preload`, exactly as static-everywhere endorses as the practical mechanism.
