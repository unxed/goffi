// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 Andrey Kolkov and GoGPU Contributors

// Command universal-respawn checks that a universal ("Profile U") binary can
// start another copy of itself the plain way and that the copy gets FFI.
//
// The re-exec bridge marks the process it re-execs with a guard variable, and
// every child inherits the environment. The guard carries the pid it was
// written for, so a child -- a new pid -- does not take its parent's guard for
// its own and runs the bridge itself. Without the pid a child started with
// exec.Command(os.Args[0]) skipped the bridge and died before main on an
// unbound libc symbol.
//
// Run without arguments, it starts itself four ways and checks each child:
//
//	os.Args[0]            (on glibc a /proc/self/fd/<n> memfd image)
//	ffi.Executable()      (the file on disk)
//	the host loader       (<loader> --preload <libs> os.Args[0], by hand)
//	a grandchild          (a child that starts its own child via os.Args[0])
//
// Each child calls getpid and strlen through goffi and reports ffi.Executable,
// which must still name the file on disk. Exit status 0 and a final
// RESPAWN-PROBE-OK line mean every child passed.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"unsafe"

	"github.com/go-webgpu/goffi/ffi"
	"github.com/go-webgpu/goffi/types"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "child":
			os.Exit(child())
		case "relay":
			out, err := exec.Command(os.Args[0], "child").CombinedOutput()
			fmt.Print(string(out))
			if err != nil {
				fmt.Printf("relay: grandchild failed: %v\n", err)
				os.Exit(1)
			}
			return
		}
	}
	os.Exit(parent())
}

func parent() int {
	if !ffi.Available() {
		fmt.Println("FAIL parent: ffi.Available() = false")
		return 1
	}
	exe, err := ffi.Executable()
	if err != nil {
		fmt.Printf("FAIL parent: ffi.Executable: %v\n", err)
		return 1
	}
	fmt.Printf("info parent pid=%d os.Args[0]=%s ffi.Executable=%s\n", os.Getpid(), os.Args[0], exe)

	ways := []struct {
		name string
		cmd  *exec.Cmd
	}{
		{"os.Args[0]", exec.Command(os.Args[0], "child")},
		{"ffi.Executable()", exec.Command(exe, "child")},
		{"host loader", exec.Command(ffi.HostLoader(), "--preload", ffi.HostPreload(), os.Args[0], "child")},
		{"grandchild", exec.Command(os.Args[0], "relay")},
	}
	failed := false
	for _, w := range ways {
		out, err := w.cmd.CombinedOutput()
		text := string(out)
		ok := err == nil && strings.Contains(text, "CHILD-OK exe="+exe+"\n")
		if ok {
			fmt.Printf("ok   %-18s\n", w.name)
		} else {
			failed = true
			fmt.Printf("FAIL %-18s err=%v\n", w.name, err)
		}
		for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
			fmt.Printf("       | %s\n", line)
		}
	}
	if failed {
		fmt.Println("RESPAWN-PROBE-FAILED")
		return 1
	}
	fmt.Println("RESPAWN-PROBE-OK")
	return 0
}

func child() int {
	if !ffi.Available() {
		fmt.Println("child: ffi.Available() = false")
		return 1
	}
	h, err := ffi.LoadLibrary(ffi.HostLibC())
	if err != nil {
		fmt.Printf("child: LoadLibrary(%s): %v\n", ffi.HostLibC(), err)
		return 1
	}

	getpid, err := ffi.GetSymbol(h, "getpid")
	if err != nil {
		fmt.Printf("child: GetSymbol(getpid): %v\n", err)
		return 1
	}
	cif := &types.CallInterface{}
	if err = ffi.PrepareCallInterface(cif, types.DefaultCall, types.SInt32TypeDescriptor, nil); err != nil {
		fmt.Printf("child: PrepareCallInterface: %v\n", err)
		return 1
	}
	var pid int32
	if _, err = ffi.CallFunction(cif, getpid, unsafe.Pointer(&pid), nil); err != nil {
		fmt.Printf("child: CallFunction(getpid): %v\n", err)
		return 1
	}
	if int(pid) != os.Getpid() {
		fmt.Printf("child: getpid() = %d, os.Getpid() = %d\n", pid, os.Getpid())
		return 1
	}

	strlen, err := ffi.GetSymbol(h, "strlen")
	if err != nil {
		fmt.Printf("child: GetSymbol(strlen): %v\n", err)
		return 1
	}
	scif := &types.CallInterface{}
	if err = ffi.PrepareCallInterface(scif, types.DefaultCall, types.UInt64TypeDescriptor,
		[]*types.TypeDescriptor{types.PointerTypeDescriptor}); err != nil {
		fmt.Printf("child: PrepareCallInterface(strlen): %v\n", err)
		return 1
	}
	s := "respawned\x00"
	sp := unsafe.Pointer(unsafe.StringData(s))
	var n uint64
	if _, err = ffi.CallFunction(scif, strlen, unsafe.Pointer(&n), []unsafe.Pointer{unsafe.Pointer(&sp)}); err != nil || n != 9 {
		fmt.Printf("child: strlen = %d, err = %v\n", n, err)
		return 1
	}

	exe, err := ffi.Executable()
	if err != nil {
		fmt.Printf("child: ffi.Executable: %v\n", err)
		return 1
	}
	fmt.Printf("child pid=%d os.Args[0]=%s\n", os.Getpid(), os.Args[0])
	fmt.Printf("CHILD-OK exe=%s\n", exe)
	return 0
}
