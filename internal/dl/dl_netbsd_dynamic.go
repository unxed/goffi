//go:build netbsd && !goffi_static

// Dynamic symbol imports for NetBSD. Kept separate from the RTLD_* constants
// so that the goffi_static build tag can drop them; see dl_linux_dynamic.go.

package dl

// Link to libc.so functions using cgo_import_dynamic.
// This works under both CGO_ENABLED=0 (where fakecgo provides the cgo runtime)
// and CGO_ENABLED=1 (where the standard runtime/cgo is linked, see cgo.go).
//
// NetBSD keeps dlopen/dlsym/dlclose in libc, and its libc SONAME carries no
// version suffix in the DT_NEEDED entry ("libc.so"), which is what fakecgo's
// symbols_netbsd.go already imports for malloc/pthread.

//go:cgo_import_dynamic goffi_dlopen dlopen "libc.so"
//go:cgo_import_dynamic goffi_dlsym dlsym "libc.so"
//go:cgo_import_dynamic goffi_dlerror dlerror "libc.so"
//go:cgo_import_dynamic goffi_dlclose dlclose "libc.so"

// Force dependency on libc.so
//go:cgo_import_dynamic _ _ "libc.so"
