//go:build !darwin && !linux

package main

import "fmt"

// 常駐の仕組みは OS ごとに違う。対応しているのは launchd(macOS)と
// systemd(Linux)だけで、それ以外では自前で常駐させてもらう。
func cmdInstallAgent([]string) error {
	return fmt.Errorf("install-agent は macOS(launchd)と Linux(systemd)のみ対応している")
}
