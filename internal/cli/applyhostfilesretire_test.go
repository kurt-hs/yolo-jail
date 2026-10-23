package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// THE CALL SITE of the `files` retire (entrypoint.RenderHostFiles): `yolo host apply --assert`,
// driven through applyHost into a scratch HOME, against a pack selected by path whose
// pack.json changes between applies — the dotfiles §25 sequence. A renamed file and a moved
// directory contribution leave the home; then a pack that declares no `files` at all has the
// rest of its old output retired too, which a render that only visits packs declaring the kind
// would never reach. A neighbouring file the user put in the load-all directory is untouched
// throughout.
func TestHostApplyRetiresFilesThePackStoppedShipping(t *testing.T) {
	local := t.TempDir()
	writeFile(t, filepath.Join(local, "files", "a.ts"), "export const a = 1\n")
	writeFile(t, filepath.Join(local, "files", "b.ts"), "export const b = 1\n")
	writeFile(t, filepath.Join(local, "files", "board", "client.ts"), "client\n")
	declare := func(contribs string) {
		writeFile(t, filepath.Join(local, "pack.json"), `{"name":"lp","contributes":[`+contribs+`]}`)
	}
	declare(`{"kind":"files","from":"files/a.ts","into":".scratch/ext/a.ts"},` +
		`{"kind":"files","from":"files/board","into":".scratch/ext/old"}`)

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("YOLO_PACK_ROOT", "")
	t.Setenv("YOLO_VERSION", "")
	selectPacksWith(t, home, `{"source":"file://`+local+`","name":"lp"}`, `,"host_management":"own"`)
	t.Chdir(t.TempDir())

	ext := filepath.Join(home, ".scratch", "ext")
	user := filepath.Join(ext, "user.ts")
	writeFile(t, user, "mine\n")

	apply := func() string {
		t.Helper()
		var out, errw bytes.Buffer
		if rc := applyHost(&out, &errw, false, true, strings.NewReader("")); rc != 0 {
			t.Fatalf("host apply rc=%d\n%s%s", rc, out.String(), errw.String())
		}
		return out.String() + errw.String()
	}
	present := func(rel string, report string) {
		t.Helper()
		if _, err := os.Lstat(filepath.Join(ext, filepath.FromSlash(rel))); err != nil {
			t.Errorf("~/.scratch/ext/%s missing: %v\n%s", rel, err, report)
		}
	}
	gone := func(rel string, report string) {
		t.Helper()
		if _, err := os.Lstat(filepath.Join(ext, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Errorf("~/.scratch/ext/%s is still in the home (%v)\n%s", rel, err, report)
		}
	}

	report := apply()
	present("a.ts", report)
	present("old/client.ts", report)

	declare(`{"kind":"files","from":"files/b.ts","into":".scratch/ext/b.ts"},` +
		`{"kind":"files","from":"files/board","into":".scratch/ext/new"}`)
	report = apply()
	gone("a.ts", report)
	gone("old", report)
	present("b.ts", report)
	present("new/client.ts", report)
	if !strings.Contains(report, filepath.Join(ext, "a.ts")) {
		t.Errorf("the removal must be disclosed by path:\n%s", report)
	}

	declare(``)
	report = apply()
	gone("b.ts", report)
	gone("new", report)

	if got, err := os.ReadFile(user); err != nil || string(got) != "mine\n" {
		t.Errorf("the user's own file was touched: %q, %v", got, err)
	}
}
