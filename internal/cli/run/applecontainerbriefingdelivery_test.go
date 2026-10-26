package run

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func TestAppleContainerBriefingDeliveryUsesFreshLaunchBytes(t *testing.T) {
	for _, tc := range []struct {
		name     string
		previous string
	}{
		{name: "fresh home"},
		{name: "replacement launch", previous: "# prior launch briefing\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, workspace := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			emptyLoopholeDirs(t)
			o := goldenOptions(workspace, home)
			packs := claudePackFixture(t)
			const cname = "yolo-ws-briefingcopy"
			agentsPath := filepath.Join(paths.AgentsDir(), cname)
			wsState := filepath.Join(workspace, ".yolo", "home")
			if err := os.MkdirAll(agentsPath, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(wsState, 0o755); err != nil {
				t.Fatal(err)
			}
			stagedBriefing := filepath.Join(agentsPath, briefingStagingName(claudeBriefingDest))
			if tc.previous != "" {
				if err := os.WriteFile(stagedBriefing, []byte(tc.previous), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			cfg := newConfig("agents", []any{"claude"}, "security", newConfig("blocked_tools", []any{}))
			in := &assembleInput{
				cfg: cfg, rt: "container", cname: cname, packs: packs,
				agentsPath: agentsPath, wsState: wsState,
				miseStore: "/mise-store", yoloVersion: "test", mountTargets: map[string]struct{}{},
			}
			o.assembleRunCmd(in)
			if in.assemblyErr != nil {
				t.Fatalf("assembleRunCmd: %v", in.assemblyErr)
			}

			staged := stagedPacks{packs: packs}
			facts := o.mechanicsFactSheet(cname, cfg, "container", staged, storePackagesPlan{}, nil)
			staging, err := o.refreshJailBriefings(cname, cfg, "container", staged,
				appliedIOPriority("container", o.IsMacOS, cfgMap(cfg, "resources")), facts)
			if err != nil {
				t.Fatalf("refreshJailBriefings: %v", err)
			}
			current, err := os.ReadFile(filepath.Join(staging, briefingStagingName(claudeBriefingDest)))
			if err != nil {
				t.Fatalf("read refreshed briefing: %v", err)
			}
			if err := materializeAppleContainerBriefings(staged.packs, staging, wsState); err != nil {
				t.Fatalf("materializeAppleContainerBriefings after refresh: %v", err)
			}
			delivered, err := os.ReadFile(filepath.Join(wsState, claudeBriefingDest))
			if err != nil {
				t.Fatalf("fresh Apple Container home has no briefing after refresh: %v", err)
			}
			if string(delivered) != string(current) {
				t.Fatalf("Apple Container home contains stale briefing bytes: got %q, want %q", delivered, current)
			}
			if strings.Contains(string(delivered), "prior launch") {
				t.Fatalf("replacement launch delivered prior briefing: %q", delivered)
			}
		})
	}
}

func TestAppleContainerBriefingMaterializationReturnsCopyErrors(t *testing.T) {
	packs := claudePackFixture(t)
	err := materializeAppleContainerBriefings(packs, t.TempDir(), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), claudeBriefingDest) {
		t.Fatalf("materializeAppleContainerBriefings error = %v, want a copy error naming %q", err, claudeBriefingDest)
	}
}

func TestRunContainerRefreshesBeforeAppleContainerBriefingDelivery(t *testing.T) {
	src, err := os.ReadFile("run.go")
	if err != nil {
		t.Fatal(err)
	}
	launch := afterFunc(string(src), "func (o *Options) runContainer(")
	assemble := strings.Index(launch, "o.assembleRunCmd(in)")
	refresh := strings.Index(launch, "o.refreshJailBriefings(cname, cfg, rt, staged,")
	deliver := strings.Index(launch, "materializeAppleContainerBriefings(staged.packs, agentsPath, wsState)")
	if assemble < 0 || refresh < 0 || deliver < 0 || !(assemble < refresh && refresh < deliver) {
		t.Fatalf("runContainer must assemble argv, refresh the produced briefing, then deliver its Apple Container copy; positions assemble=%d refresh=%d deliver=%d", assemble, refresh, deliver)
	}
	if !strings.Contains(launch[deliver:], "check that the workspace's .yolo/home is writable, then rerun `yolo`") {
		t.Fatal("runContainer's Apple Container briefing-copy refusal lost its exact repair step")
	}
}
