package config

// mountsrequestbind_test.go pins the review fixes to RC-D24 (docs/design/readable-config.md):
// what a human approves at the gate for a project's writable mount request is the host path it
// RESOLVES to, the record holds that path so a retargeted link asks again, the bind is of the
// approved path only, and every refusal runs on exactly what the gate read.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// linkedRequest is a host with the ceiling, a link OUTSIDE the workspace pointing at a harmless
// directory, and a TOML project requesting the link writable.
func linkedRequest(t *testing.T) (h mountsHost, link, harmless string) {
	t.Helper()
	h = newMountsHost(t)
	h.userTOML(t, allowMountsLines)
	root := mountSourceDir(t, "links")
	harmless = mountSourceDir(t, "harmless")
	link = filepath.Join(root, "cache")
	if err := os.Symlink(harmless, link); err != nil {
		t.Fatal(err)
	}
	h.workspaceFile(t, paths.WorkspaceConfigTOMLName, "mounts = [{ host = '"+link+"', at = '/ctx/cache', write = true }]\n")
	return h, link, harmless
}

func retarget(t *testing.T, link, to string) {
	t.Helper()
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(to, link); err != nil {
		t.Fatal(err)
	}
}

func approveRead(t *testing.T, ws string, p ChangePrompter, isTTY, accept bool) (bool, []WritableRequest) {
	t.Helper()
	read, err := ReadWorkspaceForGate(ws)
	if err != nil {
		t.Fatal(err)
	}
	ok, _, approved, err := ApproveWorkspaceRead(ws, read, nil, isTTY, accept, p)
	if err != nil {
		t.Fatal(err)
	}
	return ok, approved
}

// BLOCKER 1. The prompt names the path the request resolves to, beside the one written; the gate
// hands back the resolved path as what it approved; and a link retargeted after the approval
// changes the record's bytes, so the next launch asks again and shows the new target.
func TestApprovingARequestApprovesItsResolvedPathAndARetargetAsksAgain(t *testing.T) {
	h, link, harmless := linkedRequest(t)
	p := &reportPrompter{accept: true}
	ok, approved := approveRead(t, h.ws, p, true, false)
	if !ok || p.got == nil || len(p.got.WritableMounts) != 1 {
		t.Fatalf("ok=%v report=%+v", ok, p.got)
	}
	if line := p.got.WritableMounts[0]; !strings.Contains(line, harmless) || !strings.Contains(line, link) {
		t.Fatalf("the prompt line %q does not show both the resolved path %s and the written %s", line, harmless, link)
	}
	if len(approved) != 1 || approved[0].Resolved != harmless || approved[0].Mount.Host != link {
		t.Fatalf("approved = %+v, want the request resolved to %s", approved, harmless)
	}

	secret := filepath.Join(h.home, ".ssh")
	if err := os.MkdirAll(secret, 0o700); err != nil {
		t.Fatal(err)
	}
	retarget(t, link, secret)
	read, err := ReadWorkspaceForGate(h.ws)
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := ConfigPartChanged(h.ws, read.Config); err != nil || !changed {
		t.Fatalf("a retargeted link left the approval record matching: changed=%v err=%v", changed, err)
	}
	again := &reportPrompter{accept: false}
	if ok, _ := approveRead(t, h.ws, again, true, false); ok || again.got == nil {
		t.Fatalf("a retargeted request launched without asking: ok=%v", ok)
	}
	if !strings.Contains(strings.Join(again.got.WritableMounts, "\n"), secret) {
		t.Fatalf("the second prompt does not show the new target %s: %q", secret, again.got.WritableMounts)
	}
	if !strings.Contains(strings.Join(again.got.DiffLines, "\n"), secret) {
		t.Fatalf("the diff does not carry the new target %s: %q", secret, again.got.DiffLines)
	}
}

// BLOCKER 1, the bind's half. A request whose link moved after the gate no longer resolves to
// what was approved: Drift says so, naming both paths and the next step, and ApprovedWritable-
// Requests (what an attach reads) returns nothing for it.
func TestARequestWhoseLinkMovedSinceTheGateDrifts(t *testing.T) {
	h, link, harmless := linkedRequest(t)
	_, approved := approveRead(t, h.ws, &acceptReporter{}, false, true)
	if len(approved) != 1 || approved[0].Drift() != "" {
		t.Fatalf("approved = %+v; an unmoved request must not drift", approved)
	}
	if got := ApprovedWritableRequests(h.ws); len(got) != 1 || got[0].Resolved != harmless {
		t.Fatalf("ApprovedWritableRequests = %+v, want the approved request", got)
	}
	other := mountSourceDir(t, "other")
	retarget(t, link, other)
	why := approved[0].Drift()
	for _, want := range []string{harmless, other, "launch again"} {
		if !strings.Contains(why, want) {
			t.Fatalf("Drift() = %q, want it to name %q", why, want)
		}
	}
	if got := ApprovedWritableRequests(h.ws); len(got) != 0 {
		t.Fatalf("ApprovedWritableRequests after a retarget = %+v, want none", got)
	}
}

// P5, rule 1: the WRITTEN path is judged before any link is followed. The reviewer's repro,
// <ws>/build-cache linked to a directory outside, is refused as inside the workspace, which the
// resolved-first check let through, by validation and by the gate's own check alike.
func TestARequestWrittenInsideTheWorkspaceIsRefusedWhereverItResolves(t *testing.T) {
	h := newMountsHost(t)
	h.userTOML(t, allowMountsLines)
	outside := mountSourceDir(t, "outside")
	if err := os.Symlink(outside, filepath.Join(h.ws, "build-cache")); err != nil {
		t.Fatal(err)
	}
	h.workspaceFile(t, paths.WorkspaceConfigTOMLName,
		"mounts = [{ host = '"+filepath.Join(h.ws, "build-cache")+"', at = '/ctx/cache', write = true }]\n")
	errs, _ := h.validate(t)
	if got := joined(errs); !strings.Contains(got, "inside the workspace") || !strings.Contains(got, "can already write") {
		t.Fatalf("errs = %q, want the path written in the workspace refused", got)
	}
	read, err := ReadWorkspaceForGate(h.ws)
	if err != nil {
		t.Fatal(err)
	}
	if refusals := CheckWritableRequests(h.ws, read.WritableRequests(), nil); !strings.Contains(joined(refusals), "inside the workspace") {
		t.Fatalf("CheckWritableRequests = %q, want the same refusal on what the gate read", refusals)
	}
}

// P5 for another writable mount: rule 1 refuses a path written inside the user's own writable
// mount, and rule 2 refuses a path written outside every jail-writable tree that passes through
// a link sitting inside one.
func TestARequestThroughAnotherWritableMountIsRefused(t *testing.T) {
	h := newMountsHost(t)
	shared := mountSourceDir(t, "shared")
	outside := mountSourceDir(t, "outside")
	h.userTOML(t, allowMountsLines+"\n[[mounts]]\nhost = '"+shared+"'\nat = '/ctx/shared'\nwrite = true\n")
	if err := os.Symlink(outside, filepath.Join(shared, "c")); err != nil {
		t.Fatal(err)
	}
	// R/a is a link outside every tree, to the shared mount; R/a/c then crosses shared/c.
	r := mountSourceDir(t, "r")
	if err := os.Symlink(shared, filepath.Join(r, "a")); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, host, want string }{
		{"written inside it", filepath.Join(shared, "c"), "your writable mount"},
		{"through a link inside it", filepath.Join(r, "a", "c"), "symbolic link " + filepath.Join(shared, "c")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h.workspaceFile(t, paths.WorkspaceConfigTOMLName,
				"mounts = [{ host = '"+tc.host+"', at = '/ctx/cache', write = true }]\n")
			if got := joined(CheckWritableRequests(h.ws, gateRequests(t, h.ws), nil)); !strings.Contains(got, tc.want) {
				t.Fatalf("CheckWritableRequests = %q, want %q", got, tc.want)
			}
			errs, _ := h.validate(t)
			if got := joined(errs); !strings.Contains(got, tc.want) {
				t.Fatalf("validation = %q, want %q", got, tc.want)
			}
		})
	}
}

// MAJOR 2. CheckWritableRequests is the validation the gate runs on exactly what it read: the
// refusal set (here the home itself), the ceiling, and the broker mount fence.
func TestCheckWritableRequestsJudgesWhatTheGateRead(t *testing.T) {
	t.Run("the refusal set", func(t *testing.T) {
		h := newMountsHost(t)
		h.userTOML(t, allowMountsLines)
		h.workspaceFile(t, paths.WorkspaceConfigTOMLName, "mounts = [{ host = '~', at = '/ctx/home', write = true }]\n")
		read, err := ReadWorkspaceForGate(h.ws)
		if err != nil {
			t.Fatal(err)
		}
		reqs := read.WritableRequests()
		if len(reqs) != 1 {
			t.Fatalf("WritableRequests = %+v", reqs)
		}
		if got := joined(CheckWritableRequests(h.ws, reqs, nil)); !strings.Contains(got, "the read-write source") {
			t.Fatalf("CheckWritableRequests(~) = %q, want the home refused", got)
		}
	})
	t.Run("the ceiling", func(t *testing.T) {
		h := newMountsHost(t)
		h.userTOML(t, "packs = []\n")
		h.workspaceFile(t, paths.WorkspaceConfigTOMLName, writeRequest(mountSourceDir(t, "data")))
		read, err := ReadWorkspaceForGate(h.ws)
		if err != nil {
			t.Fatal(err)
		}
		if got := joined(CheckWritableRequests(h.ws, read.WritableRequests(), nil)); !strings.Contains(got, "[allow.mounts]") {
			t.Fatalf("CheckWritableRequests without the ceiling = %q, want it refused naming the line", got)
		}
	})
	t.Run("the broker fence", func(t *testing.T) {
		h := newMountsHost(t)
		t.Setenv("GH_CONFIG_DIR", "")
		gh := filepath.Join(h.home, ".config", "gh")
		if err := os.MkdirAll(gh, 0o755); err != nil {
			t.Fatal(err)
		}
		h.userTOML(t, allowMountsLines)
		h.workspaceFile(t, paths.WorkspaceConfigTOMLName, "mounts = [{ host = '"+gh+"', at = '/ctx/gh', write = true }]\n")
		read, err := ReadWorkspaceForGate(h.ws)
		if err != nil {
			t.Fatal(err)
		}
		if got := joined(CheckWritableRequests(h.ws, read.WritableRequests(), brokeredResolver)); !strings.Contains(got, "A workspace config may not mount it") {
			t.Fatalf("CheckWritableRequests = %q, want the fence", got)
		}
	})
}

// MINOR a. Only yolo-jail.toml or yolo-jail.local.toml itself may request: a TOML file a JSONC
// workspace includes is not the readable config, and neither is a fragment a TOML one includes.
func TestARequestFromAnIncludedFileIsRefused(t *testing.T) {
	src := mountSourceDir(t, "data")
	for _, tc := range []struct{ name, root, rootBody string }{
		{"a JSONC root", WorkspaceConfigName, `{"include_if_found": ["extra.toml"]}`},
		{"a TOML root", paths.WorkspaceConfigTOMLName, "include_if_found = ['extra.toml']\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newMountsHost(t)
			h.userTOML(t, allowMountsLines)
			h.workspaceFile(t, tc.root, tc.rootBody)
			h.workspaceFile(t, "extra.toml", writeRequest(src))
			errs, _ := h.validate(t)
			if got := joined(errs); !strings.Contains(got, "user-scope only") {
				t.Fatalf("a request in an included file was not refused: %q", got)
			}
			read, err := ReadWorkspaceForGate(h.ws)
			if err != nil {
				t.Fatal(err)
			}
			if got := read.WritableRequests(); len(got) != 0 {
				t.Fatalf("the gate read a request from an included file: %+v", got)
			}
		})
	}
}

// MINOR b. A request's host is absolute or starts with "~": a relative one would resolve
// against wherever the launcher happened to run.
func TestARelativeRequestHostIsRefused(t *testing.T) {
	_, problems := ParseMountElement(decodeMountElement(t, `{"host": "data", "at": "/ctx/data", "write": true}`), "config.mounts[0]")
	if got := joined(problems); !strings.Contains(got, "absolute") || !strings.Contains(got, "~") {
		t.Fatalf("problems = %q, want a relative writable host refused", got)
	}
	if _, problems := ParseMountElement(decodeMountElement(t, `{"host": "~/data", "at": "/ctx/data", "write": true}`), "w"); len(problems) != 0 {
		t.Fatalf("a ~ host was refused: %q", problems)
	}
}

// MINOR c. The broker allowance resolver passes over `allow.mounts`, core's own ceiling: were
// the skip deleted, the table would be reported as naming no declared broker.
func TestTheAllowanceResolverPassesOverAllowMounts(t *testing.T) {
	user := decode(t, `{"allow": {"mounts": {"on_request": ["write"]}}}`)
	if _, err := ResolveReadableAllowance(ReadableAllowanceInput{User: user}); err != nil {
		t.Fatalf("ResolveReadableAllowance refused allow.mounts: %v", err)
	}
	bad := decode(t, `{"allow": {"mounts": {"always": ["write"]}}}`)
	if _, err := ResolveReadableAllowance(ReadableAllowanceInput{User: bad}); err == nil || !strings.Contains(err.Error(), "always") {
		t.Fatalf("ResolveReadableAllowance accepted allow.mounts.always: %v", err)
	}
}

// MINOR d. `allow` is the user's ceiling: in a JSONC workspace file it is an error, as it is in a
// TOML one, and never a silent no-op.
func TestAWorkspaceAllowTableIsRefused(t *testing.T) {
	h := newMountsHost(t)
	h.workspace(t, `{"allow": {"mounts": {"on_request": ["write"]}}}`)
	errs, _ := h.validate(t)
	if got := joined(errs); !strings.Contains(got, "config.allow") || !strings.Contains(got, "user") {
		t.Fatalf("errs = %q, want a workspace `allow` refused as user-scope only", got)
	}
}
