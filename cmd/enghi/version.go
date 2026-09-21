package main

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// version はリリースビルドで -ldflags から埋める(Makefile / CI 参照)。
// go install で入れた場合はここが "dev" のままなので、
// 下の vcsRevision() が debug.ReadBuildInfo から拾い直す。
var version = "dev"

// vcsRevision はモジュール経由で入れられた場合のコミットを返す。
// バグ報告を受けるときに「どのバイナリか」を特定できないと切り分けができない。
func vcsRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	var rev, modified string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			modified = s.Value
		}
	}
	if rev == "" {
		return ""
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if modified == "true" {
		rev += "-dirty"
	}
	return rev
}

func cmdVersion([]string) error {
	v := version
	// ldflags で埋まっている場合はそこにコミットが含まれているので、重ねない。
	if v == "dev" {
		if rev := vcsRevision(); rev != "" {
			v += " (" + rev + ")"
		}
	}
	fmt.Printf("enghi %s %s/%s %s\n", v, runtime.GOOS, runtime.GOARCH, runtime.Version())
	return nil
}

// isGlobalFlag は、フラグの形で来てもサブコマンドとして扱うものを判定する。
func isGlobalFlag(s string) bool {
	switch s {
	case "-h", "--help", "-v", "--version":
		return true
	}
	return false
}
