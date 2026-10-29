package oauthbroker

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/claudeview"
)

func saveRetirementRegistration(t *testing.T, loc claudeview.Location) string {
	t.Helper()
	if err := os.MkdirAll(ViewRegistryDir, 0o700); err != nil {
		t.Fatal(err)
	}
	r := &viewRegistration{Location: loc, Runtime: "podman", LoggedOut: true, file: registrationFile(loc)}
	if err := r.save(); err != nil {
		t.Fatal(err)
	}
	return r.file
}

func TestRetireViewRegistrationRemovesOnlyExactWorkspaceMetadata(t *testing.T) {
	newViewFixture(t)
	workspace := filepath.Join(t.TempDir(), "work")
	selected := claudeview.Location{Workspace: workspace, Subdir: "claude"}
	dottedSibling := claudeview.Location{Workspace: workspace, Subdir: ".claude"}
	otherWorkspace := claudeview.Location{Workspace: workspace + "-other", Subdir: "claude"}
	selectedFile := saveRetirementRegistration(t, selected)
	dottedFile := saveRetirementRegistration(t, dottedSibling)
	otherFile := saveRetirementRegistration(t, otherWorkspace)

	removed, err := RetireViewRegistration(selected)
	if err != nil || !removed {
		t.Fatalf("RetireViewRegistration = (%v, %v), want (true, nil)", removed, err)
	}
	if _, err := os.Lstat(selectedFile); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("selected metadata remains: Lstat(%s) = %v", selectedFile, err)
	}
	for _, path := range []string{dottedFile, otherFile} {
		if _, err := os.Lstat(path); err != nil {
			t.Errorf("sibling metadata %s was changed: %v", path, err)
		}
	}
	removed, err = RetireViewRegistration(selected)
	if err != nil || removed {
		t.Fatalf("repeated retirement = (%v, %v), want (false, nil)", removed, err)
	}
}

func TestRetireViewRegistrationDoesNotFollowOrRemoveCredentialLinks(t *testing.T) {
	newViewFixture(t)
	for _, tc := range []struct {
		name string
		link func(t *testing.T, loc claudeview.Location) (string, string)
	}{
		{name: "credential leaf", link: func(t *testing.T, loc claudeview.Location) (string, string) {
			outside := filepath.Join(t.TempDir(), "outside-credentials.json")
			if err := os.WriteFile(outside, []byte("private-fake-credential"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(loc.Path()), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, loc.Path()); err != nil {
				t.Fatal(err)
			}
			return loc.Path(), outside
		}},
		{name: "workspace state ancestor", link: func(t *testing.T, loc claudeview.Location) (string, string) {
			outside := t.TempDir()
			if err := os.MkdirAll(filepath.Join(outside, "home", loc.Subdir), 0o700); err != nil {
				t.Fatal(err)
			}
			credential := filepath.Join(outside, "home", loc.Subdir, ".credentials.json")
			if err := os.WriteFile(credential, []byte("private-fake-credential"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(loc.Workspace, 0o700); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(loc.Workspace, ".yolo")
			if err := os.Symlink(outside, link); err != nil {
				t.Fatal(err)
			}
			return link, outside
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			loc := claudeview.Location{Workspace: filepath.Join(t.TempDir(), "ws"), Subdir: "claude"}
			link, target := tc.link(t, loc)
			registration := saveRetirementRegistration(t, loc)
			removed, err := RetireViewRegistration(loc)
			if err != nil || !removed {
				t.Fatalf("RetireViewRegistration = (%v, %v), want (true, nil)", removed, err)
			}
			linkTarget, err := os.Readlink(link)
			if err != nil || linkTarget != target {
				t.Errorf("credential link changed: Readlink(%s) = %q, %v; want %q", link, linkTarget, err, target)
			}
			credential := target
			if tc.name == "workspace state ancestor" {
				credential = filepath.Join(target, "home", loc.Subdir, ".credentials.json")
			}
			if _, err := os.Lstat(credential); err != nil {
				t.Errorf("credential target changed or disappeared: %v", err)
			}
			data, err := os.ReadFile(credential)
			if err != nil || string(data) != "private-fake-credential" {
				t.Errorf("credential bytes changed: %q, %v", data, err)
			}
			if _, err := os.Lstat(registration); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("registration remains: %v", err)
			}
		})
	}
}

func TestRetireViewRegistrationDoesNotCreateAbsentBrokerState(t *testing.T) {
	newViewFixture(t)
	state := filepath.Join(t.TempDir(), "not-created")
	RefreshLockPath = filepath.Join(state, "refresh.lock")
	ViewRegistryDir = filepath.Join(state, "claude-views")
	loc := claudeview.Location{Workspace: filepath.Join(t.TempDir(), "ws"), Subdir: "claude"}
	removed, err := RetireViewRegistration(loc)
	if err != nil || removed {
		t.Fatalf("never-registered retirement = (%v, %v), want (false, nil)", removed, err)
	}
	if _, err := os.Stat(state); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("never-On retirement created broker state: %v", err)
	}
}

func TestRetireViewRegistrationStopsFuturePublicationButKeepsSibling(t *testing.T) {
	f := newViewFixture(t)
	useNonDarwinHost(t)
	writeLogin(t, CanonicalPath, "AT_before", "RT_before", nowMS()+7*3600_000, nil)
	selected := f.workspace(t, "selected")
	sibling := f.workspace(t, "sibling")
	logout := []byte(`{"mcpOAuth": {}}`)
	if err := os.WriteFile(selected.Path(), logout, 0o600); err != nil {
		t.Fatal(err)
	}
	BackgroundRefreshTick(f.legacy, BackgroundRefreshLeadSeconds)
	if !registrationFor(t, selected).LoggedOut {
		t.Fatal("fixture did not persist the selected workspace's /logout")
	}

	removed, err := RetireViewRegistration(selected)
	if err != nil || !removed {
		t.Fatalf("RetireViewRegistration = (%v, %v), want (true, nil)", removed, err)
	}
	writeLogin(t, CanonicalPath, "AT_future", "RT_future", nowMS()+7*3600_000, nil)
	canonicalBefore, err := os.ReadFile(CanonicalPath)
	if err != nil {
		t.Fatal(err)
	}
	sharedBefore, sharedErr := os.ReadFile(f.legacy)
	if sharedErr != nil && !errors.Is(sharedErr, os.ErrNotExist) {
		t.Fatal(sharedErr)
	}
	selectedBefore, err := os.ReadFile(selected.Path())
	if err != nil {
		t.Fatal(err)
	}
	if string(selectedBefore) != string(logout) {
		t.Fatalf("selected view changed before publication: %s", selectedBefore)
	}

	if err := lockedErr(func() error {
		future, err := oauthFromCreds(CanonicalPath)
		if err != nil {
			return err
		}
		publishViewsLocked(future)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	canonicalAfter, err := os.ReadFile(CanonicalPath)
	if err != nil || string(canonicalAfter) != string(canonicalBefore) {
		t.Errorf("publication changed canonical bytes: err=%v", err)
	}
	sharedAfter, sharedErrAfter := os.ReadFile(f.legacy)
	if (sharedErr != nil && !errors.Is(sharedErr, os.ErrNotExist)) || sharedErrAfter != nil && !errors.Is(sharedErrAfter, os.ErrNotExist) || string(sharedAfter) != string(sharedBefore) {
		t.Errorf("publication changed shared bytes: before=(%q,%v) after=(%q,%v)", sharedBefore, sharedErr, sharedAfter, sharedErrAfter)
	}
	selectedAfter, err := os.ReadFile(selected.Path())
	if err != nil || string(selectedAfter) != string(logout) {
		t.Errorf("publication rewrote or re-enrolled retired view: %q, %v", selectedAfter, err)
	}
	assertView(t, sibling, "AT_future")
	if got := str(oauthOf(t, sibling.Path()), "refreshToken"); got != "" {
		t.Errorf("sibling view contains a refresh token after publication: %q", got)
	}
	var status strings.Builder
	if err := DescribeStore(&status); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(status.String(), selected.Path()) {
		t.Errorf("status still reports retired registration %s:\n%s", selected.Path(), status.String())
	}
	if !strings.Contains(status.String(), sibling.Path()) {
		t.Errorf("status omitted retained sibling %s:\n%s", sibling.Path(), status.String())
	}
}

func TestRetireViewRegistrationReportsRemovalFailure(t *testing.T) {
	newViewFixture(t)
	loc := claudeview.Location{Workspace: filepath.Join(t.TempDir(), "ws"), Subdir: "claude"}
	registration := saveRetirementRegistration(t, loc)
	if err := os.Remove(registration); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(registration, 0o700); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(registration, "keep")
	if err := os.WriteFile(child, []byte("metadata"), 0o600); err != nil {
		t.Fatal(err)
	}
	removed, err := RetireViewRegistration(loc)
	if err == nil || removed || !strings.Contains(err.Error(), "remove view registration") {
		t.Fatalf("RetireViewRegistration = (%v, %v), want a reported remove failure", removed, err)
	}
	if _, err := os.Stat(child); err != nil {
		t.Errorf("failed retirement changed registration contents: %v", err)
	}
}

func TestRetireViewRegistrationReportsLockFailureWithoutRemovingMetadata(t *testing.T) {
	newViewFixture(t)
	loc := claudeview.Location{Workspace: filepath.Join(t.TempDir(), "ws"), Subdir: "claude"}
	registration := saveRetirementRegistration(t, loc)
	RefreshLockPath = filepath.Join(t.TempDir(), "missing-state", "refresh.lock")
	removed, err := RetireViewRegistration(loc)
	if err == nil || removed {
		t.Fatalf("RetireViewRegistration = (%v, %v), want a lock failure", removed, err)
	}
	if _, statErr := os.Stat(registration); statErr != nil {
		t.Errorf("lock failure removed metadata: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Dir(RefreshLockPath)); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("retirement created missing broker state: %v", statErr)
	}
}
