package macosuser

import (
	"slices"
	"strings"
	"testing"
)

// The deny-by-default profile's text tests (docs/design/macos-user-model-plan.md slice 4). As
// with the old profile's, these settle what a string test can: the clauses each parameter
// renders, and ORDERING, which last-match-wins makes the whole risk. Whether the kernel
// refuses anything is integration/macosuserdenydefault_test.go's, on a Mac (slice 10).

func testProfileParams() ProfileParams {
	return ProfileParams{
		Workspace:         "/Users/you/code/alpha",
		Home:              "/Users/you/code/alpha/.yolo/home",
		HostHome:          "/Users/you",
		SandboxTmp:        "/private/var/folders/ab/xyz/T/yolo-alpha",
		DarwinUserTempDir: "/private/var/folders/ab/xyz/T",
		StagingRoot:       StagingRoot(),
		MachineDirs:       []string{"/Users/you/.local/share/yolo-jail/macos-user/.cache"},
		Loopback:          LoopbackPolicy{Ports: []int{31053}},
		Tmp:               TmpShared,
		HostSoftware:      []string{"/opt/homebrew", "/usr/local"},
	}
}

// fullProfileParams sets every optional input, so every conditional block is in the text.
func fullProfileParams() ProfileParams {
	pp := testProfileParams()
	pp.Context = []ContextLink{
		{Dest: "/ctx/lib", Source: "/opt/src/lib", Dir: true},
		{Dest: "/ctx/data", Source: "/Volumes/Data/set", RW: true, Dir: true},
	}
	pp.Relocations = []CacheRelocation{{Subdir: "huggingface", Target: "/Volumes/Data/hf"}}
	pp.ReadonlyRels = []string{"vendored"}
	pp.ReadonlyTargets = []string{"/Users/you/dotfiles/yolo-jail.jsonc"}
	pp.HomeReadonly = ResolveHomeReadonly(pp.Home, pp.Workspace, []string{".claude"},
		[]string{".claude/skills", ".claude/CLAUDE.md"})
	pp.Devices = []string{"/dev/cu.usbserial-1"}
	pp.RunAsUser = "you"
	return pp
}

func TestDenyDefaultProfileStartsFromNothing(t *testing.T) {
	p := DenyDefaultProfile(testProfileParams())
	if strings.Contains(p, "(allow default)") {
		t.Fatal("the deny-default profile allows by default")
	}
	d, a := strings.Index(p, "(deny default)"), strings.Index(p, "(allow ")
	if d < 0 || a < d {
		t.Fatalf("(deny default) must precede every allow (deny %d, first allow %d)", d, a)
	}
	for _, want := range []string{
		`(subpath "/Users/you/code/alpha")`,
		`(literal "/Users")`, `(literal "/Users/you")`, `(literal "/Users/you/code")`,
		`(subpath "/opt/homebrew")`, `(literal "/opt")`,
		`(remote ip "localhost:31053")`,
	} {
		if !strings.Contains(p, want) {
			t.Errorf("profile lacks %s", want)
		}
	}
	for _, never := range []string{`(subpath "/Users/you")`, `(subpath "/opt")`, `(subpath "/Users")`} {
		if strings.Contains(p, never) {
			t.Errorf("profile grants %s, a parent of what it needs", never)
		}
	}
}

func TestDenyDefaultProfileEndsWithTheAppleEventDeny(t *testing.T) {
	for name, pp := range map[string]ProfileParams{"minimal": testProfileParams(), "full": fullProfileParams()} {
		p := strings.TrimSpace(DenyDefaultProfile(pp))
		if !strings.HasSuffix(p, "(deny appleevent-send)") {
			t.Errorf("%s: the last rule is not (deny appleevent-send):\n%s", name, p[max(0, len(p)-200):])
		}
	}
}

func TestDenyDefaultProfileKeepsSecretsDeniedAfterAContextAllow(t *testing.T) {
	pp := testProfileParams()
	pp.Context = []ContextLink{{Dest: "/ctx/ssh", Source: "/Users/you/.ssh", Dir: true}}
	p := DenyDefaultProfile(pp)
	allow := strings.Index(p, "#seatbelt-test-id:context-read-allow#")
	deny := strings.Index(p, "#seatbelt-test-id:host-secrets-deny#")
	if allow < 0 || deny < allow {
		t.Errorf("host-secrets-deny (%d) must follow context-read-allow (%d)", deny, allow)
	}
}

// TestDenyDefaultProfileClosingBlockFollowsEveryAllow is the ordering invariant: from the
// first closing deny to the end, nothing is allowed, so no context source, relocation or
// device re-opens a keychain, a socket, a service or Apple Events.
func TestDenyDefaultProfileClosingBlockFollowsEveryAllow(t *testing.T) {
	p := DenyDefaultProfile(fullProfileParams())
	at := strings.Index(p, "#seatbelt-test-id:host-secrets-deny#")
	if at < 0 {
		t.Fatal("no host-secrets-deny in the profile")
	}
	if strings.Contains(p[at:], "(allow") {
		t.Errorf("an allow follows the closing block's first deny and can re-open it:\n%s", p[at:])
	}
	for _, id := range []string{"host-secrets-deny", "library-keychains-deny", "system-keychains-deny",
		"raw-device-deny", "macos-log-deny", "launchd-listener-deny", "ssh-agent-socket-deny",
		"container-socket-deny", "container-socket-connect-deny", "service-deny", "appleevent-send-deny"} {
		for _, allow := range []string{"context-read-allow", "context-write-allow", "cache-relocation-read-allow",
			"cache-relocation-write-allow", "device-ioctl-allow", "staging-root-read-allow", "machine-dir-allow",
			"host-software-read-allow", "loopback-port-allow"} {
			mustPrecede(t, p, "#seatbelt-test-id:"+allow+"#", "#seatbelt-test-id:"+id+"#",
				"an allow after a closing deny re-opens it (last match wins)")
		}
	}
	mustPrecede(t, p, "#seatbelt-test-id:service-deny#", "(deny appleevent-send)",
		"the Apple Events operation deny is the final form")
}

// TestDenyDefaultProfileWriteCarveOutsFollowEveryWriteAllow: workspace_readonly, the staged
// home content and the read-only context sources are terminal for writes.
func TestDenyDefaultProfileWriteCarveOutsFollowEveryWriteAllow(t *testing.T) {
	p := DenyDefaultProfile(fullProfileParams())
	first := len(p)
	for _, id := range []string{"workspace-readonly-deny", "home-content-write-deny",
		"home-content-anchor-deny", "context-readonly-deny"} {
		i := strings.Index(p, "#seatbelt-test-id:"+id+"#")
		if i < 0 {
			t.Fatalf("the full profile lacks %s", id)
		}
		first = min(first, i)
	}
	for _, ln := range strings.Split(p[first:], "\n") {
		if strings.HasPrefix(ln, "(allow") && strings.Contains(ln, "file-write") {
			t.Errorf("a write allow follows the write carve-outs and can re-open them: %s", ln)
		}
	}
}

func TestDenyDefaultProfileLoopbackPolicy(t *testing.T) {
	pp := testProfileParams()
	if p := DenyDefaultProfile(pp); !strings.Contains(p, `(deny network-outbound (remote ip "localhost:*"))`) {
		t.Error("a denying policy renders no loopback deny")
	}
	pp.Loopback = LoopbackPolicy{AllowAll: true, Ports: []int{31053}}
	if p := DenyDefaultProfile(pp); strings.Contains(p, `"localhost:`) ||
		strings.Contains(p, "loopback-deny") || strings.Contains(p, "loopback-port-allow") {
		t.Error("AllowAll still renders a loopback rule")
	}
}

func TestDenyDefaultProfileLoopbackPortsFollowTheDeny(t *testing.T) {
	pp := testProfileParams()
	pp.Loopback = LoopbackPolicy{Ports: []int{8080, 0, 31053, 70000, 8080, -1}}
	p := DenyDefaultProfile(pp)
	want := ";; #seatbelt-test-id:loopback-port-allow#\n(allow network-outbound\n" +
		"    (remote ip \"localhost:8080\")\n    (remote ip \"localhost:31053\"))\n"
	if !strings.Contains(p, want) {
		t.Errorf("the port allow is not the sorted, de-duplicated valid ports:\n%s", p)
	}
	mustPrecede(t, p, "#seatbelt-test-id:loopback-deny#", "#seatbelt-test-id:loopback-port-allow#",
		"a port allow above the loopback deny is inert")

	pp.Loopback = LoopbackPolicy{}
	p = DenyDefaultProfile(pp)
	if !strings.Contains(p, "#seatbelt-test-id:loopback-deny#") || strings.Contains(p, "loopback-port-allow") {
		t.Error("a deny with no ports must render the deny and no port allow")
	}
}

func TestDenyDefaultProfileTmpPolicyGovernsBothTempDirs(t *testing.T) {
	pp := testProfileParams()
	shared := DenyDefaultProfile(pp)
	for _, want := range []string{`(subpath "/private/tmp")`, `(subpath "/tmp")`,
		`(subpath "/private/var/folders/ab/xyz/T")`, "#seatbelt-test-id:shared-tmp-allow#"} {
		if !strings.Contains(shared, want) {
			t.Errorf("TmpShared lacks %s", want)
		}
	}
	pp.Tmp = TmpPrivate
	p := DenyDefaultProfile(pp)
	for _, never := range []string{`(subpath "/private/tmp")`, `(subpath "/private/var/folders/ab/xyz/T")`,
		`(subpath "/tmp")`, "shared-tmp-allow"} {
		if strings.Contains(p, never) {
			t.Errorf("TmpPrivate still grants %s", never)
		}
	}
	if !strings.Contains(p, `(subpath "/private/var/folders/ab/xyz/T/yolo-alpha")`) {
		t.Error("TmpPrivate lost the launch's own TMPDIR")
	}
	if !strings.Contains(p, `(literal "/private/var/folders/ab/xyz/T")`) {
		t.Error("TmpPrivate lost the traversal literal above the launch's TMPDIR")
	}
}

// The trailing slash `getconf DARWIN_USER_TEMP_DIR` returns would make a subpath that
// matches nothing.
func TestDenyDefaultProfileCleansTheUserTempDir(t *testing.T) {
	pp := testProfileParams()
	pp.DarwinUserTempDir = "/private/var/folders/ab/xyz/T/"
	if p := DenyDefaultProfile(pp); strings.Contains(p, `T/")`) ||
		!strings.Contains(p, `(subpath "/private/var/folders/ab/xyz/T")`) {
		t.Error("the user temp dir is rendered with its trailing slash")
	}
}

func TestDenyDefaultProfileHidesUsrLocalWithoutHostSoftware(t *testing.T) {
	pp := testProfileParams()
	pp.HostSoftware = nil
	p := DenyDefaultProfile(pp)
	if strings.Contains(p, `(subpath "/opt/homebrew")`) || !strings.Contains(p, `(deny file-read* (subpath "/usr/local"))`) {
		t.Error("with no host software, /opt/homebrew must be absent and /usr/local denied after /usr")
	}
	if strings.Contains(p, `(literal "/opt")`) || strings.Contains(p, `"/Applications"`) {
		t.Error("with no host software, nothing names /opt or /Applications")
	}
	mustPrecede(t, p, `(subpath "/usr")`, `(deny file-read* (subpath "/usr/local"))`,
		"the /usr/local deny is inert above the /usr allow")
}

func TestDenyDefaultProfileShowsHostSoftware(t *testing.T) {
	pp := testProfileParams()
	pp.HostSoftware = []string{"/opt/homebrew", "/usr/local", "/Applications"}
	p := DenyDefaultProfile(pp)
	if strings.Contains(p, "host-software-hide-deny") {
		t.Error("/usr/local is denied although HostSoftware names it")
	}
	want := ";; #seatbelt-test-id:host-software-read-allow#\n(allow file-read*\n" +
		"    (subpath \"/opt/homebrew\")\n    (subpath \"/usr/local\")\n    (subpath \"/Applications\"))\n"
	if !strings.Contains(p, want) {
		t.Errorf("the host software allow is not the three trees:\n%s", p)
	}
	// /opt is metadata only: its listing would show /opt/src/clientX.
	meta := ";; #seatbelt-test-id:host-software-metadata-allow#\n(allow file-read-metadata\n" +
		"    (literal \"/opt\")\n    (literal \"/usr\"))\n"
	if !strings.Contains(p, meta) {
		t.Errorf("the host software parents are not metadata-only literals:\n%s", p)
	}
}

// Context mounts sit at their original paths: every source is read, only a read-write one
// is written, and a read-only one is re-denied after every write allow.
func TestDenyDefaultProfileContextMountsReadWriteVsReadOnly(t *testing.T) {
	p := DenyDefaultProfile(fullProfileParams())
	blockOf := func(id string) string { return profileBlock(p, id) }
	if b := blockOf("context-write-allow"); !strings.Contains(b, `"/Volumes/Data/set"`) || strings.Contains(b, `"/opt/src/lib"`) {
		t.Errorf("context-write-allow must name the read-write source alone:\n%s", b)
	}
	if b := blockOf("context-readonly-deny"); !strings.Contains(b, `"/opt/src/lib"`) || strings.Contains(b, `"/Volumes/Data/set"`) {
		t.Errorf("context-readonly-deny must name the read-only source alone:\n%s", b)
	}
	if b := blockOf("context-read-allow"); !strings.Contains(b, `"/opt/src/lib"`) || !strings.Contains(b, `"/Volumes/Data/set"`) {
		t.Errorf("context-read-allow must name every source:\n%s", b)
	}
	// Traversal to each source, as literals: /opt/src is listed, its siblings are not granted.
	for _, want := range []string{`(literal "/opt/src")`, `(literal "/Volumes")`, `(literal "/Volumes/Data")`} {
		if !strings.Contains(blockOf("ancestor-literal-allow"), want) {
			t.Errorf("ancestor-literal-allow lacks %s", want)
		}
	}
	if strings.Contains(p, `(subpath "/opt/src")`) || strings.Contains(p, `(subpath "/Volumes/Data")`) {
		t.Error("a context source's parent is granted whole")
	}
}

// TestDenyDefaultProfileAncestorLiterals: every directory above a granted root is a literal,
// and none that is itself granted or under a granted root is repeated as one.
func TestDenyDefaultProfileAncestorLiterals(t *testing.T) {
	got := ancestorLiteralsAll("/Users/Shared/yolo/ws", "/Users/Shared/yolo/ws/.yolo/home", "relative", "/")
	want := "    (literal \"/Users\")\n    (literal \"/Users/Shared\")\n    (literal \"/Users/Shared/yolo\")\n"
	if got != want {
		t.Errorf("ancestorLiteralsAll:\n got %q\nwant %q", got, want)
	}
	p := DenyDefaultProfile(testProfileParams())
	b := profileBlock(p, "ancestor-literal-allow")
	for _, never := range []string{`"/Users/you/code/alpha"`, `"/Users/you/code/alpha/.yolo"`} {
		if strings.Contains(b, never) {
			t.Errorf("ancestor literals repeat a granted root: %s", never)
		}
	}
	for _, want := range []string{`(literal "/")`, `(literal "/Users/Shared")`,
		`(literal "/Users/you/.local/share/yolo-jail/macos-user")`, `(literal "/private/var/folders/ab/xyz")`} {
		if !strings.Contains(b, want) {
			t.Errorf("ancestor-literal-allow lacks %s:\n%s", want, b)
		}
	}
}

func TestDenyDefaultProfileDeviceGetsFileAccessAndIoctl(t *testing.T) {
	p := DenyDefaultProfile(fullProfileParams())
	want := ";; #seatbelt-test-id:device-ioctl-allow#\n(allow file-read* file-write* file-ioctl\n" +
		"    (literal \"/dev/cu.usbserial-1\"))\n"
	if !strings.Contains(p, want) {
		t.Errorf("a declared device lacks file access or its ioctls:\n%s", p)
	}
	if strings.Contains(DenyDefaultProfile(testProfileParams()), "device-ioctl-allow") {
		t.Error("no declared device still renders a device allow")
	}
}

// TestDenyDefaultProfileHasNoPerLanguageHomeGrant: the ruling is no per-language home grants
// (design §7), so no allow names a folder of the human's home but the locale preferences,
// the ancestors of a granted root, and paths under one.
func TestDenyDefaultProfileHasNoPerLanguageHomeGrant(t *testing.T) {
	pp := fullProfileParams()
	p := DenyDefaultProfile(pp)
	for _, dir := range []string{".nvm", ".npm", ".cargo", ".rustup", ".pyenv", ".rbenv", ".local/bin",
		".bun", ".deno", ".gradle", ".m2", ".gem", ".config", ".gitconfig", ".npmrc", "go", "Library/Caches"} {
		if strings.Contains(p, `"/Users/you/`+dir) {
			t.Errorf("the profile names ~/%s", dir)
		}
	}
	granted := []string{pp.Workspace, pp.Home}
	granted = append(granted, pp.MachineDirs...)
	for _, f := range allowForms(p) {
		for _, q := range quoted(f) {
			if !strings.HasPrefix(q, "/Users/you/") {
				continue
			}
			switch {
			case slices.ContainsFunc(granted, func(g string) bool { return within(q, g) || within(g, q) }):
			case strings.HasPrefix(q, "/Users/you/Library/Preferences/.GlobalPreferences"),
				strings.HasPrefix(q, "/Users/you/Library/Preferences/com.apple.GlobalPreferences"),
				q == "/Users/you/.CFUserTextEncoding":
			default:
				t.Errorf("an allow names %s in the human's home:\n%s", q, f)
			}
		}
	}
}

// TestDenyDefaultProfileParensBalance: an unbalanced form makes sandbox-exec refuse the whole
// profile, which no Linux test would otherwise notice.
func TestDenyDefaultProfileParensBalance(t *testing.T) {
	variants := map[string]ProfileParams{"minimal": testProfileParams(), "full": fullProfileParams()}
	alt := fullProfileParams()
	alt.Loopback, alt.Tmp, alt.HostSoftware = LoopbackPolicy{AllowAll: true}, TmpPrivate, nil
	variants["alternate rulings"] = alt
	variants["empty"] = ProfileParams{}
	odd := fullProfileParams()
	odd.Workspace = `/Users/you/code/a "quoted" (dir)\x`
	odd.HostHome = `/Users/y(o)u`
	variants["hostile spelling"] = odd
	for name, pp := range variants {
		if err := checkSBPLBalance(DenyDefaultProfile(pp)); err != "" {
			t.Errorf("%s: %s", name, err)
		}
	}
}

// The user the profile runs as changes no rule: one profile, both modes (design §7).
func TestDenyDefaultProfileRulesDoNotDependOnTheRunAsUser(t *testing.T) {
	a, b := fullProfileParams(), fullProfileParams()
	a.RunAsUser, b.RunAsUser = "you", "_yolojail"
	pa, pb := DenyDefaultProfile(a), DenyDefaultProfile(b)
	if !strings.Contains(pa, ";; Run as: you.") || !strings.Contains(pb, ";; Run as: _yolojail.") {
		t.Error("the header does not name the run-as user")
	}
	if stripComments(pa) != stripComments(pb) {
		t.Error("the rules differ between the two run-as users")
	}
}

func TestDenyDefaultProfileCarriesTheSafehouseAttribution(t *testing.T) {
	p := DenyDefaultProfile(testProfileParams())
	for _, want := range []string{"https://github.com/eugene1g/agent-safehouse",
		"Apache License,\n;; Version 2.0", "http://www.apache.org/licenses/LICENSE-2.0", "Modified by yolo-jail"} {
		if !strings.Contains(p, want) {
			t.Errorf("the profile lacks the attribution text %q", want)
		}
	}
	if !strings.HasPrefix(p, "(version 1)\n") {
		t.Error("the profile does not open with (version 1)")
	}
}

func TestWithProductionRulingsSetsThePrototypeValues(t *testing.T) {
	pp := ProfileParams{Loopback: LoopbackPolicy{AllowAll: true, Ports: []int{4000}}, Tmp: TmpPrivate}.WithProductionRulings()
	if pp.Loopback.AllowAll != productionLoopback.AllowAll || pp.Tmp != productionTmp ||
		!slices.Equal(pp.HostSoftware, productionHostSoftware) {
		t.Errorf("WithProductionRulings did not set the production values: %+v", pp)
	}
	if !slices.Equal(pp.Loopback.Ports, []int{4000}) {
		t.Errorf("the caller's ports were lost: %v", pp.Loopback.Ports)
	}
	pp.HostSoftware[0] = "/mutated"
	if productionHostSoftware[0] == "/mutated" {
		t.Error("WithProductionRulings shares the production slice")
	}
	// The prototype's choices, until slice 11 records the rulings.
	if productionLoopback.AllowAll || productionTmp != TmpShared ||
		!slices.Equal(productionHostSoftware, []string{"/opt/homebrew", "/usr/local", "/Applications"}) {
		t.Error("a production ruling moved off the prototype's value before slice 11")
	}
}

// --- helpers ---

// sbplScan walks profile text outside comments and strings, calling visit for every other
// rune with its byte offset. Strings are "…" with backslash escapes, and #"…" regex literals.
func sbplScan(s string, visit func(i int, r byte)) (unterminated bool) {
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == ';':
			for i < len(s) && s[i] != '\n' {
				i++
			}
		case c == '"' || (c == '#' && i+1 < len(s) && s[i+1] == '"'):
			if c == '#' {
				i++
			}
			for i++; i < len(s) && s[i] != '"'; i++ {
				if s[i] == '\\' {
					i++
				}
			}
			if i >= len(s) {
				return true
			}
		default:
			visit(i, c)
		}
	}
	return false
}

func checkSBPLBalance(p string) string {
	depth, bad := 0, ""
	if sbplScan(p, func(i int, c byte) {
		switch c {
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 && bad == "" {
				bad = "a ')' closes nothing near: " + p[max(0, i-80):i+1]
			}
		}
	}) {
		return "an unterminated string"
	}
	if bad != "" {
		return bad
	}
	if depth != 0 {
		return "unbalanced parentheses"
	}
	return ""
}

// allowForms returns every top-level form that starts with "(allow".
func allowForms(p string) []string {
	var forms []string
	depth, start := 0, -1
	sbplScan(p, func(i int, c byte) {
		switch c {
		case '(':
			if depth == 0 {
				start = i
			}
			depth++
		case ')':
			depth--
			if depth == 0 && start >= 0 && strings.HasPrefix(p[start:], "(allow") {
				forms = append(forms, p[start:i+1])
			}
		}
	})
	return forms
}

// quoted returns the "…" strings in a form, unescaped.
func quoted(f string) []string {
	var out []string
	for rest := f; ; {
		i := strings.Index(rest, `"`)
		if i < 0 {
			return out
		}
		var b strings.Builder
		j := i + 1
		for ; j < len(rest) && rest[j] != '"'; j++ {
			if rest[j] == '\\' && j+1 < len(rest) {
				j++
			}
			b.WriteByte(rest[j])
		}
		out = append(out, b.String())
		if j >= len(rest) {
			return out
		}
		rest = rest[j+1:]
	}
}

// stripComments drops every comment and blank line.
func stripComments(p string) string {
	var b strings.Builder
	for _, ln := range strings.Split(p, "\n") {
		if i := strings.Index(ln, ";;"); i >= 0 {
			ln = ln[:i]
		}
		if ln = strings.TrimSpace(ln); ln != "" {
			b.WriteString(ln + "\n")
		}
	}
	return b.String()
}
