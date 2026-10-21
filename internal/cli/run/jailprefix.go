package run

import (
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
)

// jailprefix.go selects the paired Linux helper bundle mounted into the jail.
// The source bundle and helpers are two read-only mounts so the in-jail yolo
// resolves the flake exe-relative from /opt/yolo-jail/bin.
const (
	JailPrefixDir      = "/opt/yolo-jail"
	JailPrefixBinDir   = JailPrefixDir + "/bin"
	JailPrefixShareDir = JailPrefixDir + "/share/yolo-jail"
	JailEntrypointPath = JailPrefixBinDir + "/yolo-entrypoint"
)

type jailPrefix struct {
	binDir   string
	shareDir string
}

// This is the existing helper set shared by flake.nix and
// scripts/stage-source-bundle.sh. Refuse partial bundles before mounting them.
var requiredJailBinaries = []string{
	"yolo", "yolo-entrypoint", "yolo-jaild", "yolo-ps", "yolo-cglimit",
	"yolo-journalctl", "yolo-serial", "yolo-log",
}

// prebuiltBinDir is the per-arch Linux helper directory in a bundle. A Darwin
// host still needs Linux helpers because its container jail is Linux.
func prebuiltBinDir(root string) string {
	return filepath.Join(root, "bin", "linux-"+goruntime.GOARCH)
}

// resolveJailPrefix accepts only the complete Linux helper set paired with root.
// Launches never compile helpers: an incomplete bundle refuses with the build-only
// or install remedy, before a jail can start with missing binaries.
func (o *Options) resolveJailPrefix(root, rt string) (jailPrefix, bool) {
	p, ok := o.jailPrefixSource(root)
	if !ok {
		missing := missingJailBinaries(prebuiltBinDir(root))
		o.pr(o.Stderr).printf("[bold red]Cannot start jail: the selected bundle at %s is missing Linux helper binaries: %s.[/bold red]\n"+
			"Launches do not build helpers. Run `just build-dev-bundle`, then invoke the printed launcher by path, or run `just install` for the installed bundle.",
			root, strings.Join(missing, ", "))
		return jailPrefix{}, false
	}
	if msg := prefixUnreachableFromVM(p, rt, o.IsMacOS, o.Getenv("YOLO_NIX_HOST_DAEMON")); msg != "" {
		o.pr(o.Stderr).print(msg)
		return jailPrefix{}, false
	}
	if msg := o.unsharedBindSources(rt, []string{p.binDir, p.shareDir}); msg != "" {
		o.pr(o.Stderr).print(msg)
		return jailPrefix{}, false
	}
	return p, true
}

func missingJailBinaries(dir string) []string {
	var missing []string
	for _, name := range requiredJailBinaries {
		info, err := os.Lstat(filepath.Join(dir, name))
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			missing = append(missing, name)
		}
	}
	return missing
}

// jailPrefixSource selects only the complete prebuilt set next to the flake.
// An incomplete adjacent bundle cannot fall through to source compilation.
func (o *Options) jailPrefixSource(root string) (jailPrefix, bool) {
	prebuilt := prebuiltBinDir(root)
	if len(missingJailBinaries(prebuilt)) != 0 {
		return jailPrefix{}, false
	}
	return jailPrefix{binDir: resolveSymlinks(prebuilt), shareDir: resolveSymlinks(root)}, true
}

// resolveSymlinks names immutable generation paths rather than a mutable active
// bundle symlink; failure to resolve only affects the disclosure path.
func resolveSymlinks(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// jailPrefixMountArgs keeps the helper and flake bundle mounts read-only.
func jailPrefixMountArgs(p jailPrefix) []string {
	return []string{
		"-v", p.binDir + ":" + JailPrefixBinDir + ":ro",
		"-v", p.shareDir + ":" + JailPrefixShareDir + ":ro",
	}
}

func describeJailPrefix(p jailPrefix) string {
	return p.binDir + " (prebuilt, from the flake bundle)"
}

// prefixUnreachableFromVM preserves the macOS Podman Machine guard for a
// bundle stored under /nix, which the default VM does not share. Apple Container
// has no fixed machine share list; explicit YOLO_NIX_HOST_DAEMON remains the
// operator's assertion that the Podman VM shares /nix.
func prefixUnreachableFromVM(p jailPrefix, rt string, isMacOS bool, nixOptIn string) string {
	if !isMacOS {
		return ""
	}
	if rt == "container" { // parity: NotApplicable — Apple Container has no fixed machine VM share set and binds the named host path
		return ""
	}
	if envTruthy(nixOptIn) {
		return ""
	}
	var unreachable []string
	for _, dir := range []string{p.binDir, p.shareDir} {
		if underDir(dir, hostNixStore) {
			unreachable = append(unreachable, dir)
		}
	}
	if len(unreachable) == 0 {
		return ""
	}
	return "[bold red]Cannot start jail: the selected helper bundle is in the nix store, which this Mac's runtime VM does not share.[/bold red]\n" +
		"  " + strings.Join(unreachable, "\n  ") + "\n" +
		"Two ways forward:\n" +
		"  • Share /nix with the VM and say so — `podman machine init -v /nix:/nix`\n" +
		"    and export YOLO_NIX_HOST_DAEMON=1.\n" +
		"  • Build a matched bundle with `just build-dev-bundle` and invoke the printed\n" +
		"    launcher by path, or use `just install` for a bundle under $HOME."
}

// underDir compares path segments, so /nix/store-of-my-own is not mistaken for
// a location inside /nix/store.
func underDir(path, dir string) bool {
	if path == dir {
		return true
	}
	return strings.HasPrefix(path, dir+string(filepath.Separator))
}
