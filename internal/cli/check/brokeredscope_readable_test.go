package check

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/brokerscope"
	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/loopholes"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func TestCheckBrokeredScopeUsesSelectedReadableDeclaration(t *testing.T) {
	for _, tc := range []struct {
		source, brokerName, owner, remoteHost, readCapability, grantKind, capability, request string
		hasRemote                                                                             bool
		wantGrants                                                                            int
		wantScopeChanged                                                                      *bool
	}{
		{"git", "git-broker", "git-owner", "*", "fetch", "branch", "push", `push = ["main"]`, true, 2, nil},
		{"github", "github-broker", "github-owner", "github.com", "read", "create-pr", "create_pr", `create_pr = true`, true, 1, nil},
		{"git", "git-broker", "git-owner", "*", "fetch", "branch", "push", `push = ["main"]`, false, 0, boolPtr(false)},
		{"github", "github-broker", "github-owner", "github.com", "read", "create-pr", "create_pr", `repos = ["acme/outside"]
create_pr = true`, false, 1, boolPtr(true)},
	} {
		t.Run(fmt.Sprintf("%s/remote=%t", tc.source, tc.hasRemote), func(t *testing.T) {
			checkScopeReadableBroker(t, tc.source, tc.brokerName, tc.owner, tc.remoteHost, tc.readCapability, tc.grantKind, tc.capability, tc.request, tc.hasRemote, tc.wantGrants, tc.wantScopeChanged)
		})
	}
}

func boolPtr(v bool) *bool { return &v }

func checkScopeReadableBroker(t *testing.T, source, brokerName, owner, remoteHost, readCapability, grantKind, capability, request string, hasRemote bool, wantGrants int, wantScopeChanged *bool) {
	t.Helper()
	root := t.TempDir()
	module := filepath.Join(root, brokerName)
	if err := os.MkdirAll(module, 0o700); err != nil {
		t.Fatal(err)
	}
	pending := ""
	if capability == "create_pr" {
		pending = `,"pending_capabilities":["create_pr"]`
	}
	manifest := fmt.Sprintf(`{"name":%q,"transport":"loopback-tls","lifecycle":"spawned",
		"host_daemon":{"cmd":["/bin/true","{socket}","{repository_scope}"],"publishes":"socket"},
		"brokered":{"source":%q,"remote_host":%q,"grants":[%q],"read_capability":%q,"kind_capabilities":{%q:%q}%s}}`, brokerName, source, remoteHost, grantKind, readCapability, grantKind, capability, pending)
	if err := os.WriteFile(filepath.Join(module, "manifest.jsonc"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	authModule := filepath.Join(root, "aws-auth")
	if err := os.MkdirAll(authModule, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(authModule, "manifest.jsonc"), []byte(`{"name":"aws-auth","settings":{"profile":{"type":"string","scope":"user"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	inactiveModule := filepath.Join(root, "inactive-broker")
	if err := os.MkdirAll(inactiveModule, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inactiveModule, "manifest.jsonc"), []byte(`{"name":"inactive-broker","transport":"loopback-tls","lifecycle":"spawned","host_daemon":{"cmd":["/bin/true","{socket}","{repository_scope}"],"publishes":"socket"},"brokered":{"source":"inactive","remote_host":"github.com"},"platforms":["windows"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	restore := loopholes.SnapshotPackModules()
	t.Cleanup(restore)
	loopholes.SetPackModules([]loopholes.PackModule{{Dir: module, Pack: owner, HostExecApproved: true}, {Dir: authModule, Pack: "aws-auth", HostExecApproved: true}, {Dir: inactiveModule, Pack: "inactive-owner", HostExecApproved: true}})

	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("YOLO_USER_LAYER", "")
	userConfig := filepath.Join(home, ".config", "yolo-jail")
	if err := os.MkdirAll(userConfig, 0o700); err != nil {
		t.Fatal(err)
	}
	userBody := fmt.Sprintf("[allow.%s]\nalways = [%q]\non_request = [%q]\n\n[allow.aws-auth]\nprofile = \"prod\"\n", source, readCapability, capability)
	if err := os.WriteFile(filepath.Join(userConfig, paths.UserConfigTOMLName), []byte(userBody), 0o600); err != nil {
		t.Fatal(err)
	}

	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if hasRemote || remoteHost == "*" {
		if err := os.MkdirAll(filepath.Join(workspace, ".git"), 0o700); err != nil {
			t.Fatal(err)
		}
		remoteConfig := ""
		if hasRemote {
			remoteConfig = "[remote \"origin\"]\n\turl = https://github.com/org/repo.git\n"
		}
		if err := os.WriteFile(filepath.Join(workspace, ".git", "config"), []byte(remoteConfig), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(workspace, config.WorkspaceTOMLConfigName), []byte(fmt.Sprintf("[requests.%s]\n%s\n", source, request)), 0o600); err != nil {
		t.Fatal(err)
	}
	wantRemoteCount := 0
	if hasRemote {
		wantRemoteCount = 1
	}
	if remoteHost == "*" {
		if remote := brokerscope.ReadAllRemotes(workspace); len(remote.Remotes) != wantRemoteCount {
			t.Fatalf("fixture remotes = %+v", remote)
		}
	} else if remote := brokerscope.ReadRemotes(workspace, remoteHost); len(remote.Remotes) != wantRemoteCount {
		t.Fatalf("fixture remotes = %+v", remote)
	}
	read, err := config.ReadWorkspaceForGate(workspace)
	if err != nil {
		t.Fatal(err)
	}
	merged := jsonx.NewOrderedMap()
	loopholesConfig := jsonx.NewOrderedMap()
	brokerConfig := jsonx.NewOrderedMap()
	brokerConfig.Set("enabled", true)
	loopholesConfig.Set(brokerName, brokerConfig)
	merged.Set("loopholes", loopholesConfig)

	got := brokeredScopeForCheck(workspace, merged, "podman", read)
	if got == nil || len(got.Sources) != 1 {
		t.Fatalf("check scope = %+v, want one active %s broker", got, source)
	}
	if got.Fatal != nil {
		t.Fatalf("readable selected broker declaration failed: %v", got.Fatal)
	}
	if got.ReadableInput == nil {
		t.Fatalf("check scope bypassed the canonical readable gate: %+v", got.Sources[0])
	}
	if got.Sources[0].Source != source || len(got.Sources[0].Grants) != wantGrants {
		t.Fatalf("check scope = %+v, want %d concrete %s grants", got.Sources[0], wantGrants, source)
	}
	if profile, ok := got.ReadableInput.PackSettings["aws-auth"]["profile"]; !ok || profile != "prod" {
		t.Fatalf("selected aws-auth setting = %#v, want prod", got.ReadableInput.PackSettings)
	}
	if len(got.ReadableInput.Sources) != 1 || got.ReadableInput.Sources[0].Source != source {
		t.Fatalf("readable sources = %+v, want only active %s broker", got.ReadableInput.Sources, source)
	}
	if capability == "create_pr" {
		foundPending := false
		for _, request := range got.ReadableInput.Requests {
			foundPending = foundPending || (request.Capability == "create_pr" && request.Status == config.ReadableRequestPending)
		}
		if !foundPending {
			t.Fatalf("GitHub create_pr request = %+v, want pending-runtime, with no grant", got.ReadableInput.Requests)
		}
	}
	if wantScopeChanged != nil {
		report, err := config.ScopeChanges(workspace, got)
		if err != nil {
			t.Fatal(err)
		}
		if report.ScopeChanged != *wantScopeChanged {
			t.Fatalf("scope changed = %t, want %t; report %+v", report.ScopeChanged, *wantScopeChanged, report)
		}
	}
}
