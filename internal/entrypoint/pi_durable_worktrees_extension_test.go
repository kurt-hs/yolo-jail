package entrypoint

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/worktreereap"
)

// pi-subagents' worktrees default to os.tmpdir(), which a jail deletes on exit — the one
// shipped workflow tool whose default is per-launch (docs/reference/jail-home.md#storage-classes-and-the-durable-dir
// §2.4). The pi pack delivers an extension into pi's discovery directory that points it at
// $YOLO_DURABLE_DIR/worktrees/pi-subagents (DS-D4). The declaration is asserted from the
// shipped pack, so a source file with no files contribution fails here.
func TestShippedPiPackDeliversTheDurableWorktreesExtension(t *testing.T) {
	p := shippedPiPack(t)
	var found bool
	for _, c := range p.Decl.Contributions() {
		if c.Kind == packdecl.KindFiles && c.From == "extensions/yolo-durable-worktrees.js" &&
			c.Into == ".pi/agent/extensions/yolo-durable-worktrees.js" {
			found = true
		}
	}
	if !found {
		t.Fatal("the pi pack does not deliver yolo-durable-worktrees.js to pi's extension directory")
	}
	home := t.TempDir()
	if _, err := RenderHostFiles(p, home, filesReq(t), false); err != nil {
		t.Fatalf("rendering the shipped pi extensions: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".pi", "agent", "extensions", "yolo-durable-worktrees.js")); err != nil {
		t.Fatalf("pi cannot discover the rendered extension: %v", err)
	}
}

// Executed: the extension configures worktreeBaseDir in subagent/config.json from
// YOLO_DURABLE_DIR when the user has not set it, and changes nothing when either is
// otherwise (a user's own value wins; the host notch and a launch with no durable dir export
// no YOLO_DURABLE_DIR). PI_SUBAGENTS_WORKTREE_DIR is never exported into the process
// environment, so child processes do not inherit it.
func TestPiDurableWorktreesExtensionSetsTheBaseDirOnlyWhenUnset(t *testing.T) {
	p := shippedPiPack(t)
	source, err := os.ReadFile(filepath.Join(p.Root, "extensions", "yolo-durable-worktrees.js"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "extension.mjs"), source, 0o644); err != nil {
		t.Fatal(err)
	}
	harness := filepath.Join(dir, "harness.mjs")
	if err := os.WriteFile(harness, []byte(`
import extension, { subagentConfigPath } from "./extension.mjs";
import fs from "node:fs";

await extension({});

const envVal = process.env.PI_SUBAGENTS_WORKTREE_DIR ?? "<unset>";
const configPath = subagentConfigPath(process.env);
let fileVal = "<no-file>";
if (fs.existsSync(configPath)) {
	try {
		const parsed = JSON.parse(fs.readFileSync(configPath, "utf8"));
		fileVal = parsed.worktreeBaseDir ?? "<no-key>";
	} catch (e) {
		fileVal = "<parse-error>";
	}
}
process.stdout.write(JSON.stringify({ env: envVal, file: fileVal }));
`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		env      []string
		initFile string
		wantEnv  string
		wantFile string
	}{
		{
			name:     "durable dir, no user value",
			env:      []string{"YOLO_DURABLE_DIR=@D"},
			wantEnv:  "<unset>",
			wantFile: "@D/worktrees/pi-subagents",
		},
		{
			name:     "a user's own env value wins",
			env:      []string{"YOLO_DURABLE_DIR=@D", "PI_SUBAGENTS_WORKTREE_DIR=/mine"},
			wantEnv:  "/mine",
			wantFile: "<no-file>",
		},
		{
			name:     "no durable dir",
			env:      nil,
			wantEnv:  "<unset>",
			wantFile: "<no-file>",
		},
		{
			name:     "a user's own worktreeBaseDir in config.json wins",
			env:      []string{"YOLO_DURABLE_DIR=@D"},
			initFile: `{"worktreeBaseDir": "/user/configured"}`,
			wantEnv:  "<unset>",
			wantFile: "/user/configured",
		},
		{
			name:     "an existing yolo-managed worktreeBaseDir updates when durable dir changes",
			env:      []string{"YOLO_DURABLE_DIR=@D"},
			initFile: `{"worktreeBaseDir": "/old/path", "_yoloManagedWorktreeBaseDir": true}`,
			wantEnv:  "<unset>",
			wantFile: "@D/worktrees/pi-subagents",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			if tc.initFile != "" {
				cfgPath := filepath.Join(home, ".pi", "agent", "extensions", "subagent", "config.json")
				if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(cfgPath, []byte(tc.initFile), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command(requireNode(t, "the pi durable-worktrees extension"), harness)
			cmd.Dir = dir
			// A durable dir of the test's own, never the jail's: the extension also starts the
			// worktree reaper on the directory it sets (here absent, so it starts none).
			durableDir := filepath.Join(home, "durable")
			cmd.Env = environWithout("YOLO_DURABLE_DIR", "PI_SUBAGENTS_WORKTREE_DIR", "PI_CODING_AGENT_DIR")
			cmd.Env = append(cmd.Env, "HOME="+home)
			for _, kv := range tc.env {
				cmd.Env = append(cmd.Env, strings.ReplaceAll(kv, "@D", durableDir))
			}
			wantFile := strings.ReplaceAll(tc.wantFile, "@D", durableDir)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("executing the extension: %v\n%s", err, out)
			}
			var got struct {
				Env  string `json:"env"`
				File string `json:"file"`
			}
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatalf("parsing output %q: %v", out, err)
			}
			if got.Env != tc.wantEnv {
				t.Errorf("PI_SUBAGENTS_WORKTREE_DIR in env = %q, want %q", got.Env, tc.wantEnv)
			}
			if got.File != wantFile {
				t.Errorf("worktreeBaseDir in config.json = %q, want %q", got.File, wantFile)
			}
		})
	}
}

// The extension bounds the directory it set: loading it (pi does at every start, every
// workflow child included) starts `yolo-jaild reap-worktrees` on that directory, detached,
// at most once per interval, and never on a directory the user chose. Observed through a fake
// yolo-jaild on PATH that records its arguments; delete the startReap call and this fails.
func TestPiDurableWorktreesExtensionStartsTheReaperOnItsOwnDirectory(t *testing.T) {
	node := requireNode(t, "the pi durable-worktrees extension")
	p := shippedPiPack(t)
	source, err := os.ReadFile(filepath.Join(p.Root, "extensions", "yolo-durable-worktrees.js"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "extension.mjs"), source, 0o644); err != nil {
		t.Fatal(err)
	}
	harness := filepath.Join(dir, "harness.mjs")
	if err := os.WriteFile(harness, []byte("import extension from \"./extension.mjs\";\nawait extension({});\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	run := func(t *testing.T, polls int, ws string, extraEnv ...string) (args string, started bool) {
		t.Helper()
		home := t.TempDir()
		bin := t.TempDir()
		record := filepath.Join(bin, "args")
		script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + record + ".tmp && mv " + record + ".tmp " + record + "\n"
		if err := os.WriteFile(filepath.Join(bin, "yolo-jaild"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(node, harness)
		cmd.Dir = dir
		cmd.Env = append(environWithout("YOLO_DURABLE_DIR", "PI_SUBAGENTS_WORKTREE_DIR", "PI_CODING_AGENT_DIR", "PATH", "HOME"),
			"HOME="+home, "PATH="+bin+":/usr/bin:/bin", "YOLO_DURABLE_DIR="+filepath.Join(ws, ".yolo", "durable"))
		cmd.Env = append(cmd.Env, extraEnv...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("executing the extension: %v\n%s", err, out)
		}
		// Detached: give it a moment to write (a negative case waits less, a second).
		for range polls {
			if b, err := os.ReadFile(record); err == nil {
				return string(b), true
			}
			time.Sleep(100 * time.Millisecond)
		}
		return "", false
	}
	newWorkspace := func(t *testing.T) (ws, base string) {
		ws = t.TempDir()
		base = filepath.Join(ws, ".yolo", "durable", "worktrees", "pi-subagents")
		if err := os.MkdirAll(base, 0o755); err != nil {
			t.Fatal(err)
		}
		return ws, base
	}

	t.Run("due", func(t *testing.T) {
		ws, base := newWorkspace(t)
		args, started := run(t, 50, ws)
		if !started {
			t.Fatal("loading the extension started no reaper on the directory it set")
		}
		want := strings.Join([]string{"reap-worktrees", "--base", base, "--prefix", "pi-worktree-", "--repo", ws}, "\n") + "\n"
		if args != want {
			t.Fatalf("the reaper was started with\n%s\nwant\n%s", args, want)
		}
		if _, err := os.Stat(filepath.Join(base, ".yolo-reap.json")); err != nil {
			t.Fatalf("the rate-limit stamp was not written: %v", err)
		}
	})
	t.Run("a recent pass", func(t *testing.T) {
		ws, base := newWorkspace(t)
		if err := os.WriteFile(filepath.Join(base, ".yolo-reap.json"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, started := run(t, 10, ws); started {
			t.Fatal("a reaper started though a pass ran within the interval")
		}
	})
	t.Run("the user's own directory", func(t *testing.T) {
		ws, _ := newWorkspace(t)
		if _, started := run(t, 10, ws, "PI_SUBAGENTS_WORKTREE_DIR="+filepath.Join(ws, "mine")); started {
			t.Fatal("a reaper started on a worktree directory the user chose")
		}
	})
}

// The extension's names are the reaper's: the report it stamps is the one the reaper writes.
func TestPiDurableWorktreesExtensionStampsTheReapersReport(t *testing.T) {
	p := shippedPiPack(t)
	source, err := os.ReadFile(filepath.Join(p.Root, "extensions", "yolo-durable-worktrees.js"))
	if err != nil {
		t.Fatal(err)
	}
	if want := `export const REAP_REPORT = "` + worktreereap.ReportName + `";`; !strings.Contains(string(source), want) {
		t.Fatalf("the extension does not stamp %s, the reaper's report", worktreereap.ReportName)
	}
}
