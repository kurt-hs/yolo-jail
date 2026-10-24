// A stand-in yolo build for scripts/test_launcher_slot.py and scripts/test-undeploy.sh.
//
// The tests copy this file to cmd/yolo (and cmd/other) of a throwaway module named
// github.com/mschulkind-oss/yolo-jail and build it, so the binary carries the same Go build info
// a real yolo build does: launcher-slot.py identifies a launcher by that build info alone. The
// -X ldflags set what `--version --full` reports. Every other invocation is appended to
// $HOME/fake-yolo.log as one line, the invoked name followed by the arguments.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	stamp    = "v1-0-gabc"
	kind     = "installed"
	identity = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

func main() {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "--version" {
		fmt.Printf("build stamp: %s\ncommit: abc\nbinary sha256: x\nsnapshot identity: %s\nbuild kind: %s\n",
			stamp, identity, kind)
		return
	}
	home := os.Getenv("HOME")
	record(filepath.Join(home, "fake-yolo.log"), filepath.Base(os.Args[0])+" "+strings.Join(args, " "))
	switch strings.Join(args, " ") {
	case "internal bundle-dir":
		fmt.Println(filepath.Join(home, ".local/share/yolo-jail/flake-bundle"))
	case "host-daemon status --format json":
		fmt.Println(`[{"loophole": "alpha", "running": true}, {"loophole": "beta", "running": false}]`)
	}
	if len(args) == 3 && args[0] == "host-daemon" && args[1] == "stop" {
		record(filepath.Join(filepath.Dir(home), "stop.log"), "stop "+args[2])
	}
}

func record(path, line string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintln(f, line)
}
