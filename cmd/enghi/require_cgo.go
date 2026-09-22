//go:build !cgo

// This file is compiled only when CGO_ENABLED=0, and it always fails to build.
//
// Without cgo, go-sqlite3 still compiles as a stub that fails at run time, so a
// broken binary would quietly end up in a release. Note that Go defaults
// CGO_ENABLED to 0 when cross compiling, so a build across GOOS stops here —
// build on each OS's own runner.
package main

func init() {
	enghi_requires_cgo__set_CGO_ENABLED_1()
}
