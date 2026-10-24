package ghbroker

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/brokerscope"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func TestLeaseCrashHelper(t *testing.T) {
	if os.Getenv("PILE7_LEASE_CRASH") != "1" {
		return
	}
	sf := brokerscope.File{Grants: readAll, Source: Source, LaunchID: "crash", PID: os.Getpid(), Container: "crash-container", Workspace: "/fixture", Repos: []string{"o/r"}}
	path, err := brokerscope.Write(sf)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := BindAuthority(path, sf, "crashed-start"); err != nil {
		t.Fatal(err)
	}
	os.Exit(0) // no close/defer: model abrupt exit; kernel must release the lease
}
func TestNextBrokerCollectsOnlyProvenInertOwnedLeases(t *testing.T) {
	a, _ := authorityFixture(t)
	live := a.lease.Name()
	cmd := exec.Command(os.Args[0], "-test.run=^TestLeaseCrashHelper$")
	cmd.Env = append(os.Environ(), "PILE7_LEASE_CRASH=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(string(out), err)
	}
	dir := paths.BrokerSourceDir(Source)
	crashed := filepath.Join(dir, ".lifetime-crashed-start")
	if _, err := os.Stat(crashed); err != nil {
		t.Fatal(err)
	}
	unknown := map[string]string{
		".lifetime-empty":    "",
		".lifetime-corrupt":  "{",
		".lifetime-unknown":  `{"version":999,"source":"github","start":"unknown","launch_id":"crash"}`,
		".lifetime-mismatch": `{"version":1,"source":"github","start":"different","launch_id":"crash"}`,
		".lifetime-extra":    `{"version":1,"source":"github","start":"extra","launch_id":"crash","unexpected":true}`,
	}
	for name, data := range unknown {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(dir, ".lifetime-symlink")
	if err := os.Symlink(crashed, link); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(dir, ".lifetime-fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	// Exercise the actual next broker start, not just a standalone test collector.
	next, err := BindAuthority(a.path, a.scope, "next-start")
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if _, err := os.Stat(crashed); !os.IsNotExist(err) {
		t.Fatal("inert lease not collected", err)
	}
	for name := range unknown {
		if _, err := os.Lstat(filepath.Join(dir, name)); err != nil {
			t.Fatal("unknown lease removed", name, err)
		}
	}
	for _, path := range []string{live, link, fifo} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatal("live/symlink removed", err)
		}
	}
}

func TestConcurrentBrokerLeaseCreationAndCollectionKeepsEveryLiveLease(t *testing.T) {
	a, _ := authorityFixture(t)
	var wg sync.WaitGroup
	results := make(chan *Authority, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			b, err := BindAuthority(a.path, a.scope, fmt.Sprintf("parallel-%d", i))
			if err != nil {
				t.Errorf("binding: %v", err)
				return
			}
			results <- b
		}(i)
	}
	wg.Wait()
	close(results)
	for b := range results {
		if _, err := os.Stat(b.lease.Name()); err != nil {
			t.Error("live lease collected", err)
		}
		b.Close()
	}
	if _, err := os.Stat(a.lease.Name()); err != nil {
		t.Fatal("original live lease collected", err)
	}
}
