// esgw 是唯一二进制入口；子命令随冲刺增加（compile → serve/bootstrap → ...）。
package main

import (
	"fmt"
	"os"

	"github.com/linkinghack/envoy-standalone-gateway/internal/version"
)

func main() {
	if len(os.Args) >= 2 && os.Args[1] == "version" {
		fmt.Printf("%s %s\n", version.BinaryName, version.Version)
		return
	}
	fmt.Fprintf(os.Stderr, "usage: %s <command> [flags]\n\nCommands:\n  version   print version\n", version.BinaryName)
	os.Exit(2)
}
