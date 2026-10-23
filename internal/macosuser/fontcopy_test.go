package macosuser

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// fontCopyFixture is a source font folder with two fonts, one in a subfolder, and a root to copy
// into, with a run that records the grant.
func fontCopyFixture(t *testing.T) (src, root string, grants *[][]string, run func([]string) int) {
	t.Helper()
	base := t.TempDir()
	src, root = filepath.Join(base, "Library", "Fonts"), filepath.Join(base, "staged", "fonts")
	writeFontFile(t, filepath.Join(src, "Company.ttf"), "company")
	writeFontFile(t, filepath.Join(src, "family", "Family.otf"), "family")
	if err := os.MkdirAll(filepath.Dir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	grants = &[][]string{}
	run = func(argv []string) int {
		mu.Lock()
		defer mu.Unlock()
		*grants = append(*grants, argv)
		return 0
	}
	return src, root, grants, run
}

func writeFontFile(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func copyEntries(t *testing.T, root string) []string {
	t.Helper()
	e, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, x := range e {
		names = append(names, x.Name())
	}
	return names
}

// THE COPY: the fonts land under a digest folder, 0700/0600, the root carries the account's
// inheriting read entry (granted once, when it is made), a relaunch reuses the copy, and a
// changed source makes a new one and removes the old.
func TestStageFontCopyCopiesReusesAndRefreshes(t *testing.T) {
	src, root, grants, run := fontCopyFixture(t)
	c := stageFontCopy(src, root, run)
	if c.Err != "" || c.Dir == "" || filepath.Dir(c.Dir) != root {
		t.Fatalf("stageFontCopy = %+v", c)
	}
	for rel, want := range map[string]string{"Company.ttf": "company", "family/Family.otf": "family"} {
		b, err := os.ReadFile(filepath.Join(c.Dir, rel))
		if err != nil || string(b) != want {
			t.Errorf("copy of %s = %q, %v", rel, b, err)
		}
		if fi, _ := os.Stat(filepath.Join(c.Dir, rel)); fi == nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("copy of %s is not 0600: %v", rel, fi)
		}
	}
	if fi, _ := os.Stat(root); fi == nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("the copy root is not 0700: %v", fi)
	}
	// One grant, on the root under its build name beside the final one (it is renamed into place
	// already granted).
	if len(*grants) != 1 {
		t.Fatalf("grants = %v, want the root's one inheriting entry", *grants)
	}
	g := (*grants)[0]
	if built := g[len(g)-1]; filepath.Dir(built) != filepath.Dir(root) ||
		!strings.HasPrefix(filepath.Base(built), ".fonts"+fontCopyNewPrefix) ||
		fmt.Sprint(g) != fmt.Sprint(fontCopyGrant(built)) {
		t.Errorf("grant = %v, want fontCopyGrant on the root's build folder", g)
	}
	for _, g := range *grants {
		if strings.HasPrefix(g[len(g)-1], filepath.Dir(src)) {
			t.Errorf("a grant lands in the source's home: %v", g)
		}
	}

	again := stageFontCopy(src, root, run)
	if again.Dir != c.Dir || len(*grants) != 1 {
		t.Errorf("a relaunch with an unchanged source: %+v, grants %v; want the same copy, no new grant", again, *grants)
	}

	writeFontFile(t, filepath.Join(src, "New.ttf"), "new")
	fresh := stageFontCopy(src, root, run)
	if fresh.Dir == c.Dir || fresh.Dir == "" {
		t.Fatalf("a changed source reused the old copy: %+v", fresh)
	}
	if got := copyEntries(t, root); !slices.Equal(got, []string{filepath.Base(fresh.Dir)}) {
		t.Errorf("copies under the root = %v; want only the new one", got)
	}

	removeFontCopy(root)
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Errorf("removeFontCopy left %s: %v", root, err)
	}
}

// THE SOURCE IS NOT FOLLOWED: a source folder that is a link is not copied, and a link inside it
// is skipped and counted, so a link to ~/.ssh cannot ride along.
func TestStageFontCopyFollowsNoLinks(t *testing.T) {
	src, root, _, run := fontCopyFixture(t)
	secret := filepath.Join(t.TempDir(), "id_ed25519")
	writeFontFile(t, secret, "SECRET")
	if err := os.Symlink(secret, filepath.Join(src, "Evil.ttf")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(secret), filepath.Join(src, "evil-dir")); err != nil {
		t.Fatal(err)
	}
	c := stageFontCopy(src, root, run)
	if c.Dir == "" || c.Links != 2 {
		t.Fatalf("stageFontCopy = %+v; want a copy with 2 links skipped", c)
	}
	_ = filepath.Walk(c.Dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			if b, _ := os.ReadFile(p); string(b) == "SECRET" {
				t.Errorf("a linked file was copied: %s", p)
			}
		}
		return nil
	})

	linked := filepath.Join(t.TempDir(), "Fonts")
	if err := os.Symlink(src, linked); err != nil {
		t.Fatal(err)
	}
	if l := stageFontCopy(linked, filepath.Join(t.TempDir(), "fonts"), run); !l.SourceIsLink || l.Dir != "" {
		t.Errorf("a linked source folder: %+v; want it refused", l)
	}
}

// A file yolo cannot read is named, not copied, and the rest still is.
func TestStageFontCopyNamesUnreadableFiles(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a 0000 file")
	}
	src, root, _, run := fontCopyFixture(t)
	locked := filepath.Join(src, "Locked.ttf")
	writeFontFile(t, locked, "locked")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	c := stageFontCopy(src, root, run)
	if c.Dir == "" || !slices.Equal(c.Unreadable, []string{"Locked.ttf"}) {
		t.Errorf("stageFontCopy = %+v; want Locked.ttf named and the rest copied", c)
	}
}

// A root the account cannot be granted read on is no copy, and is not left behind.
func TestStageFontCopyRefusesWithoutTheGrant(t *testing.T) {
	src, root, _, _ := fontCopyFixture(t)
	c := stageFontCopy(src, root, func([]string) int { return 1 })
	if c.Dir != "" || c.Err == "" {
		t.Errorf("stageFontCopy with a failed grant = %+v", c)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Errorf("an ungranted root was left behind")
	}
}

// SESSIONS DO NOT RACE: launches refreshing at once all end on one complete copy, and no
// half-built folder is left; a stale build is swept by the next refresh.
func TestStageFontCopyConcurrentLaunchesAgree(t *testing.T) {
	src, root, _, run := fontCopyFixture(t)
	var wg sync.WaitGroup
	dirs := make([]string, 8)
	for i := range dirs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			dirs[i] = stageFontCopy(src, root, run).Dir
		}(i)
	}
	wg.Wait()
	for _, d := range dirs {
		if d == "" || d != dirs[0] {
			t.Fatalf("concurrent copies disagree: %v", dirs)
		}
	}
	if b, err := os.ReadFile(filepath.Join(dirs[0], "family", "Family.otf")); err != nil || string(b) != "family" {
		t.Errorf("the shared copy is incomplete: %q %v", b, err)
	}
	stale := filepath.Join(root, fontCopyNewPrefix+"dead")
	if err := os.Mkdir(stale, 0o700); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * fontCopyStaleAfter)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	writeFontFile(t, filepath.Join(src, "More.ttf"), "more")
	stageFontCopy(src, root, run)
	if got := copyEntries(t, root); len(got) != 1 || strings.HasPrefix(got[0], fontCopyNewPrefix) {
		t.Errorf("after a refresh the root holds %v; want the one new copy", got)
	}
}
