package packsrc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

func TestParseSourceDefinitionsAndQualifiedPackReferences(t *testing.T) {
	local := jsonx.NewOrderedMap()
	local.Set("dotfiles", "~/src/yolo-packs")
	git := jsonx.NewOrderedMap()
	git.Set("git", "https://github.com/acme/yolo-packs.git")
	definitions := jsonx.NewOrderedMap()
	localPath, _ := local.Get("dotfiles")
	definitions.Set("dotfiles", localPath)
	definitions.Set("acme", git)

	sources, err := ParseSourceDefinitions(definitions)
	if err != nil {
		t.Fatal(err)
	}
	if got := sources["acme"].Repo; got != "https://github.com/acme/yolo-packs" {
		t.Fatalf("normalized git source = %q", got)
	}

	ref, err := ParsePackReference("dotfiles/matt", sources, "/home/user/.config/yolo-jail/packs", "/home/user", "config.packs[0]", nil)
	if err != nil {
		t.Fatal(err)
	}
	if ref.Name != "dotfiles/matt" || ref.SourceID != "dotfiles" || ref.LocalPath != "/home/user/src/yolo-packs/matt" ||
		ref.Source != "file:///home/user/src/yolo-packs/matt" {
		t.Fatalf("local reference = %+v", ref)
	}

	ref, err = ParsePackReference("acme/tools", sources, "", "/home/user", "config.packs[1]", nil)
	if err != nil {
		t.Fatal(err)
	}
	if ref.Name != "acme/tools" || ref.SourceID != "acme" || !ref.AllowDefaultRef || ref.Source != "git+https://github.com/acme/yolo-packs//tools" {
		t.Fatalf("tree reference = %+v", ref)
	}

	ref, err = ParsePackReference("local/matt", sources, "/home/user/.config/yolo-jail/packs", "/home/user", "config.packs[2]", nil)
	if err != nil {
		t.Fatal(err)
	}
	if ref.Name != "local/matt" || ref.SourceID != "local" || ref.LocalPath != "/home/user/.config/yolo-jail/packs/matt" {
		t.Fatalf("conventional local reference = %+v", ref)
	}
}

func TestParsePackReferenceAllowsOnlyKnownShippedBareName(t *testing.T) {
	got, err := ParsePackReference("claude", nil, "", "/home/user", "config.packs[0]", map[string]bool{"claude": true})
	if err != nil || got.Source != "embedded:claude" || got.Name != "claude" || got.SourceID != "shipped" {
		t.Fatalf("shipped reference = %+v, %v", got, err)
	}
	if _, err := ParsePackReference("not-shipped", nil, "", "/home/user", "config.packs[1]", nil); err == nil {
		t.Fatal("an unshipped bare name was accepted")
	}
}

func TestParseDirectGitPackUsesRepositoryIdentityAndExplicitRef(t *testing.T) {
	object := jsonx.NewOrderedMap()
	object.Set("git", "https://github.com/mschulkind-oss/matt-craft.git")
	object.Set("ref", "alice/review")
	ref, err := ParsePackReference(object, nil, "", "/home/user", "config.packs[0]", nil)
	if err != nil {
		t.Fatal(err)
	}
	if ref.Name != "mschulkind-oss/matt-craft" || ref.Source != "git+https://github.com/mschulkind-oss/matt-craft?ref=alice%2Freview" || ref.AllowDefaultRef {
		t.Fatalf("direct git reference = %+v", ref)
	}
	// The manifest name is not part of this contract: identity is derived from the
	// address and the reference contains no field from pack.toml/pack.json.
}

func TestParseDirectGitRejectsNestedRepositoryPath(t *testing.T) {
	object := jsonx.NewOrderedMap()
	object.Set("git", "https://git.example/group/subgroup/repo")
	if _, err := ParsePackReference(object, nil, "", "/home/user", "config.packs[0]", nil); err == nil ||
		!strings.Contains(err.Error(), "owner/repository URL") {
		t.Fatalf("nested direct-git URL error = %v, want explicit owner/repository requirement", err)
	}
}

func TestParsePackReferenceRejectsUnqualifiedAndUnknownHandles(t *testing.T) {
	for _, tc := range []struct {
		value any
		want  string
	}{
		{"not-shipped", "only shipped packs"},
		{"missing/tools", "unknown pack source"},
		{"../tools", "expected source/pack"},
	} {
		_, err := ParsePackReference(tc.value, nil, "/packs", "/home/user", "config.packs[4]", nil)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("ParsePackReference(%v) error = %v, want %q", tc.value, err, tc.want)
		}
	}
}

func TestLocalDirectoryIdentityFollowsSymlinks(t *testing.T) {
	dir := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(dir, alias); err != nil {
		t.Fatal(err)
	}
	one, err := LocalDirectoryIdentity(dir)
	if err != nil {
		t.Fatal(err)
	}
	two, err := LocalDirectoryIdentity(alias)
	if err != nil {
		t.Fatal(err)
	}
	if one != two {
		t.Fatalf("identities differ: %q != %q", one, two)
	}
}
