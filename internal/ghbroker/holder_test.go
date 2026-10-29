package ghbroker

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/brokeraudit"
	"github.com/mschulkind-oss/yolo-jail/internal/brokerstore"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// TestAnotherLaunchsEndpointFilesAndSpendsNothing: the holder comes from this broker's own
// launch, gated by the crossing identity, so a connection through an endpoint another launch
// published can neither file a request for this launch, read its status, nor spend its grant.
func TestAnotherLaunchsEndpointFilesAndSpendsNothing(t *testing.T) {
	for form, foreignDir := range productionEndpointDirs("another-container") {
		t.Run(form, func(t *testing.T) {
			b, n := windowFixture(t)
			own, _ := frontedEndpoint(t, b, filepath.Base(paths.HostServicesDir(authorityContainer, false)))
			foreign, _ := frontedEndpoint(t, b, foreignDir)
			if got := Forward(writeReq().Argv, foreign); got == 0 || got == ExitTempFail {
				t.Fatalf("a write through another launch's endpoint: exit %d, want a refusal", got)
			}
			if rs, _, _ := b.store.List(); len(rs) != 0 {
				t.Fatalf("another launch's connection filed %+v", rs)
			}
			if got := Forward(writeReq().Argv, own); got != ExitTempFail {
				t.Fatalf("own write: exit %d", got)
			}
			rs, _, _ := b.store.List()
			if len(rs) != 1 {
				t.Fatal(rs)
			}
			if got := Forward([]string{"status", rs[0].ID}, foreign); got == 0 || got == ExitTempFail {
				t.Fatalf("another launch read this launch's request status: exit %d", got)
			}
			if _, err := b.store.Decide(rs[0].ID, "pending", brokerstore.Decision{}); err != nil {
				t.Fatal(err)
			}
			if got := Forward(writeReq().Argv, foreign); got == 0 || n.Load() != 0 {
				t.Fatalf("another launch spent this launch's grant: exit %d, ran %d", got, n.Load())
			}
			if got := Forward(writeReq().Argv, own); got != 0 || n.Load() != 1 {
				t.Fatalf("own approved write: exit %d, ran %d", got, n.Load())
			}
		})
	}
}

func TestLaunchHolderMapsOnlyThisLaunchsEndpointDirs(t *testing.T) {
	const c = authorityContainer
	hash := paths.JailShortHash(c)
	other := paths.JailShortHash("another-container")
	for crossing, want := range map[string]string{
		filepath.Base(paths.HostServicesDir(c, false)): hash,
		paths.HostServicesSessionPrefix(c) + "q7Zx01":  hash,
		// Not this launch's, or not an endpoint dir at all: unchanged, so never this holder.
		paths.HostServicesDirName(other):                  paths.HostServicesDirName(other),
		paths.HostServicesSessionPrefix("another") + "a1": paths.HostServicesSessionPrefix("another") + "a1",
		paths.HostServicesSessionPrefix(c):                paths.HostServicesSessionPrefix(c),
		paths.HostServicesDirName(hash) + "x":             paths.HostServicesDirName(hash) + "x",
		"unknown":                                         "unknown",
	} {
		if got := launchHolder(c, crossing); got != want {
			t.Errorf("launchHolder(%q) = %q, want %q", crossing, got, want)
		}
	}
	if got := launchHolder("", paths.HostServicesDirName(hash)); got != paths.HostServicesDirName(hash) {
		t.Errorf("a launch with no container mapped its crossing to %q", got)
	}
}

// TestStandingBranchAcceptsTheProductionHolder: window's standing branch compares the same
// holder, so a production crossing identity passes it and another launch's does not.
func TestStandingBranchAcceptsTheProductionHolder(t *testing.T) {
	read := Request{Argv: []string{"pr", "view", "1"}, Repo: "o/r"}
	for _, tc := range []struct {
		crossing string
		ok       bool
	}{
		{filepath.Base(paths.HostServicesDir(authorityContainer, false)), true},
		{paths.HostServicesSessionPrefix(authorityContainer) + "q7Zx01", true},
		{paths.HostServicesDirName(paths.JailShortHash("another-container")), false},
	} {
		b, n := windowFixture(t)
		d, _, err := b.authority.Classify(read.Argv, read.Repo)
		if err != nil || d.Outcome != OutcomeStanding {
			t.Fatalf("fixture read is not standing: %+v %v", d, err)
		}
		var ev brokeraudit.Event
		code, msg := b.window(context.Background(), read, b.holder(tc.crossing), d, func([]byte) {}, func([]byte) {}, &ev)
		if ran := n.Load() == 1; ran != tc.ok || (code == 0) != tc.ok {
			t.Fatalf("%s: exit %d %q, ran %v, want ran %v", tc.crossing, code, msg, ran, tc.ok)
		}
		if !tc.ok && !strings.Contains(msg, "standing holder mismatch") {
			t.Fatalf("%s: %q", tc.crossing, msg)
		}
	}
}
