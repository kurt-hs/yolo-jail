package run

// macosoverlaylinks_test.go pins that NO macos-user copy of pack content into the sandbox reads a
// host file through a symbolic link (docs/reference/macos-user-nix-and-features.md, CD-1). The
// home overlay is copied by the host user, outside the sandbox, so a copy that read through a
// link would hand the sandbox account the bytes of any file the host user can read. Two layers:
//
//   - the overlay copy itself keeps every link as the same link (it never reads through one), so
//     a link that reaches it is resolved later by the sandbox account, with that account's own
//     rights, exactly as `cp -R` then carries it into the staged root and the guest pack tree;
//   - a FETCHED pack's link out of its own tree never reaches that copy at all: staging refuses
//     it (packstage's no-escape rule), on this backend as on the container ones, and an in-tree
//     link arrives as the file it names.

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jailcontent"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/macosuser"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// linkSecret is the body of the host file the fixtures' absolute links name.
const linkSecret = "HOST-SECRET-BYTES-0f3c"

// writeLinkSecret writes a host "secret" outside every tree the launch stages and returns its path.
func writeLinkSecret(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(p, []byte(linkSecret), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// treeHoldsSecret names every regular file under root whose bytes carry linkSecret. It reads
// with Lstat semantics (WalkDir never follows a link), so a link to the secret is not a finding:
// only a COPY of its bytes is.
func treeHoldsSecret(t *testing.T, root string) []string {
	t.Helper()
	var hits []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return nil
		}
		if b, err := os.ReadFile(p); err == nil && bytes.Contains(b, []byte(linkSecret)) {
			hits = append(hits, p)
		}
		return nil
	})
	return hits
}

// THE OVERLAY COPY NEVER READS THROUGH A LINK, for a skills destination and an ordinary pack
// `files` tree alike, through the real overlay builder: an absolute link to a host file, a
// relative file link and a relative directory link each arrive as the same link, and the host
// file's bytes appear nowhere in the overlay. Red with the overlay copying skills and files by
// reading each entry (the old copyTree, which read the secret's bytes into the overlay).
func TestMacosUserOverlayCopiesPackContentLinksAsLinks(t *testing.T) {
	staging := t.TempDir()
	secret := writeLinkSecret(t)

	// A staged skills dir and a staged pack `files` tree, each holding the three kinds of link.
	skillSrc := filepath.Join(staging, "skills-acme")
	filesSrc := filepath.Join(staging, "packs", "acme", "tools")
	links := map[string]string{
		"leak":                        secret,
		"alias.md":                    "real.md",
		"sub-link":                    "sub",
		filepath.Join("sub", "up.md"): "../real.md",
	}
	for _, root := range []string{filepath.Join(skillSrc, "demo"), filesSrc} {
		if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "real.md"), []byte("real"), 0o644); err != nil {
			t.Fatal(err)
		}
		for rel, target := range links {
			if err := os.Symlink(target, filepath.Join(root, rel)); err != nil {
				t.Fatal(err)
			}
		}
	}

	overlay, dests, err := buildMacosHomeOverlayFor(staging,
		[]jailcontent.SkillTarget{{Staging: "skills-acme", Dest: ".claude/skills"}}, nil,
		[]packFilesTarget{{Pack: "acme", Src: filesSrc, Dest: ".acme/tools",
			Root: filepath.Join(staging, "packs", "acme"), From: "tools"}}, nil)
	if err != nil {
		t.Fatalf("building the overlay: %v", err)
	}
	if len(dests) != 2 {
		t.Fatalf("the overlay delivered %v, want the skills and the files destination", dests)
	}
	for _, root := range []string{filepath.Join(overlay, ".claude", "skills", "demo"), filepath.Join(overlay, ".acme", "tools")} {
		for rel, want := range links {
			if got, err := os.Readlink(filepath.Join(root, rel)); err != nil || got != want {
				t.Errorf("%s reached the overlay as %q (%v), want the link to %q", filepath.Join(root, rel), got, err, want)
			}
		}
		if got, err := os.ReadFile(filepath.Join(root, "real.md")); err != nil || string(got) != "real" {
			t.Errorf("%s/real.md = %q (%v), want the regular file copied", root, got, err)
		}
	}
	if hits := treeHoldsSecret(t, overlay); len(hits) > 0 {
		t.Errorf("the host file an absolute link named was copied into the overlay at %v", hits)
	}
}

// A FILE THAT IS NEITHER A REGULAR FILE, A DIRECTORY NOR A LINK is refused by the overlay copy
// rather than skipped, since skipping it would report a partial delivery as a whole one.
func TestMacosUserOverlayRefusesAnIrregularPackFile(t *testing.T) {
	staging := t.TempDir()
	filesSrc := filepath.Join(staging, "tools")
	if err := os.MkdirAll(filesSrc, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(filesSrc, "pipe"), 0o644); err != nil {
		t.Skipf("cannot make a FIFO here: %v", err)
	}
	_, _, err := buildMacosHomeOverlayFor(staging, nil, nil,
		[]packFilesTarget{{Pack: "acme", Src: filesSrc, Dest: ".acme/tools"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "pipe") {
		t.Fatalf("building an overlay over a FIFO returned %v, want a refusal naming it", err)
	}
}

// fetchedLinkPackManifest is a fetched pack delivering a `files` tree and a skill.
const fetchedLinkPackManifest = `{"name": "acme", "contributes": [
	{"kind": "files", "from": "tools", "into": ".acme/tools"}]}`

// A FETCHED PACK'S LINK TO A HOST FILE IS REFUSED BEFORE ANY COPY, through Run's macos-user arm:
// the launch ends naming the link, the sandbox is never started, and the secret's bytes are in
// nothing the launch staged under the home or the workspace. A link in a `files` tree and one in
// a skill are each refused on their own.
func TestMacosUserRefusesAFetchedPacksLinkToAHostFile(t *testing.T) {
	for name, link := range map[string]string{"files": "tools/leak", "skills": "skills/demo/leak.md"} {
		t.Run(name, func(t *testing.T) {
			home := packHome(t)
			ws := t.TempDir()
			secret := writeLinkSecret(t)
			src := fetchedPackSourceWithLinks(t, map[string]string{
				"pack.json":            fetchedLinkPackManifest,
				"tools/real.txt":       "real",
				"skills/demo/SKILL.md": "---\nname: demo\ndescription: d\n---\n# Demo\n",
			}, map[string]string{link: secret})
			writeUserConfigJSON(t, home, `{"packs": ["claude", {"name": "acme", "source": "`+src+`"}]}`)

			ran := false
			got := macosUserLaunchWithOptions(t, ws, nil, func(o *Options) {
				backend := o.MacosUserRun
				o.MacosUserRun = func(cfg *jsonx.OrderedMap, a string, b, c []string, d, e string, overlay macosuser.HomeOverlay,
					hc macosuser.HostContext, dry bool, env *jsonx.OrderedMap, bt []packload.BlockedTool, jd macosuser.JailDaemons) int {
					ran = true
					return backend(cfg, a, b, c, d, e, overlay, hc, dry, env, bt, jd)
				}
			})
			if got.rc == 0 || ran {
				t.Fatalf("Run() = %d with the sandbox started %v, want a refusal before it:\n%s", got.rc, ran, got.out)
			}
			if !strings.Contains(got.out, "symlink pointing outside the pack") || !strings.Contains(got.out, link) {
				t.Errorf("the refusal does not name the escaping link %s:\n%s", link, got.out)
			}
			for _, root := range []string{home, ws} {
				if hits := treeHoldsSecret(t, root); len(hits) > 0 {
					t.Errorf("the host file the pack's link named was copied to %v", hits)
				}
			}
		})
	}
}

// A FETCHED PACK'S LINK INSIDE ITS OWN TREE STILL WORKS: the launch runs, and the overlay the
// sandbox is handed carries the file the link names, under the link's name.
func TestMacosUserDeliversAFetchedPacksInTreeLink(t *testing.T) {
	home := packHome(t)
	ws := t.TempDir()
	src := fetchedPackSourceWithLinks(t, map[string]string{
		"pack.json":      fetchedLinkPackManifest,
		"tools/real.txt": "real",
	}, map[string]string{"tools/alias.txt": "real.txt"})
	writeUserConfigJSON(t, home, `{"packs": ["claude", {"name": "acme", "source": "`+src+`"}]}`)

	var alias []byte
	var readErr error
	got := macosUserLaunchWithOptions(t, ws, nil, func(o *Options) {
		backend := o.MacosUserRun
		o.MacosUserRun = func(cfg *jsonx.OrderedMap, a string, b, c []string, d, e string, overlay macosuser.HomeOverlay,
			hc macosuser.HostContext, dry bool, env *jsonx.OrderedMap, bt []packload.BlockedTool, jd macosuser.JailDaemons) int {
			alias, readErr = os.ReadFile(filepath.Join(overlay.Tree, ".acme", "tools", "alias.txt"))
			return backend(cfg, a, b, c, d, e, overlay, hc, dry, env, bt, jd)
		}
	})
	if got.rc != 0 {
		t.Fatalf("Run() = %d, want 0:\n%s", got.rc, got.out)
	}
	if readErr != nil || string(alias) != "real" {
		t.Errorf("~/.acme/tools/alias.txt in the overlay = %q (%v), want the bytes of the file it names", alias, readErr)
	}
}

// NO OVERLAY WRITE GOES THROUGH A LINK AN EARLIER DESTINATION LAID: a patched extension's tree
// keeps its links, absolute ones included, so its `x -> <host file>` and `d -> <host dir>` sit in
// the overlay when a later `files` contribution at the same destination writes a regular file at
// `x` and one under `d`. The host user doing that copy must not truncate, write or chmod the host
// file, nor create a file in the host directory. Red with the overlay writing by plain path.
func TestMacosUserOverlayNeverWritesThroughALinkItLaid(t *testing.T) {
	staging := t.TempDir()
	hostDir := t.TempDir()
	hostFile := filepath.Join(hostDir, "zshrc")
	if err := os.WriteFile(hostFile, []byte("host rc"), 0o600); err != nil {
		t.Fatal(err)
	}
	tree := filepath.Join(staging, "tree")
	plain := filepath.Join(staging, "plain")
	for _, dir := range []string{tree, filepath.Join(plain, "d")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, target := range map[string]string{"x": hostFile, "d": hostDir} {
		if err := os.Symlink(target, filepath.Join(tree, name)); err != nil {
			t.Fatal(err)
		}
	}
	for rel, body := range map[string]string{"x": "PACK BYTES", filepath.Join("d", "planted"): "PACK BYTES"} {
		if err := os.WriteFile(filepath.Join(plain, rel), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	_, _, err := buildMacosHomeOverlayFor(staging, nil, nil, []packFilesTarget{
		{Pack: "acme", Src: tree, Dest: ".acme", Tree: "ext"},
		{Pack: "acme", Src: plain, Dest: ".acme"},
	}, nil)
	if err == nil {
		t.Errorf("an overlay writing a file through a link to a host path was built without a refusal")
	}
	if got, _ := os.ReadFile(hostFile); string(got) != "host rc" {
		t.Errorf("the host file a laid link names now holds %q", got)
	}
	if fi, err := os.Stat(hostFile); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("the host file's mode changed: %v %v", fi.Mode(), err)
	}
	if _, err := os.Lstat(filepath.Join(hostDir, "planted")); err == nil {
		t.Errorf("a pack file was created in the host directory a laid link names")
	}
}
