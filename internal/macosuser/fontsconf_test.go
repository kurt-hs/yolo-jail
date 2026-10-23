package macosuser

// fontsconf_test.go pins the sandbox's fonts (fontsconf.go): OQ-NX4's core row (a generated
// fonts.conf FONTCONFIG_FILE names), the human's ~/Library/Fonts in the run-as-you mode, and the
// share_host_fonts opt-in for the separate account (OQ-NX9). The launch is driven through
// RunMacosUser, so deleting its wiring fails a test here.

import (
	"bytes"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

const testHostHome = "/Users/you"

// fontDeps answers the font host reads: which folders exist, the MobileAsset glob, a real path.
func fontDeps(dirs ...string) Deps {
	d := mockDeps(nil)
	d.PathIsDir = func(p string) bool {
		for _, x := range dirs {
			if x == p {
				return true
			}
		}
		return false
	}
	d.Glob = func(pattern string) []string {
		switch pattern {
		case "/System/Library/AssetsV2/com_apple_MobileAsset_Font*":
			return []string{"/System/Library/AssetsV2/com_apple_MobileAsset_Font9",
				"/System/Library/AssetsV2/com_apple_MobileAsset_Font10"}
		case "/System/Library/Assets/com_apple_MobileAsset_Font*":
			return []string{"/System/Library/Assets/com_apple_MobileAsset_Font3"}
		}
		return nil
	}
	d.EvalSymlinks = func(p string) (string, bool) {
		if strings.HasPrefix(p, "/System/Library/AssetsV2/") {
			return "/private/var/MobileAsset/AssetsV2/" + strings.TrimPrefix(p, "/System/Library/AssetsV2/"), true
		}
		return p, true
	}
	d.StageFontCopy = func(src, root string) FontCopy {
		return FontCopy{Dir: root + "/" + testFontCopyDigest}
	}
	return d
}

// testFontCopyDigest is the copy folder fontDeps' StageFontCopy answers with.
const testFontCopyDigest = "0123456789abcdef"

// testFontCopy is that copy's path.
func testFontCopy() string { return fontCopyRoot() + "/" + testFontCopyDigest }

func fontDarwin() *Darwin {
	return &Darwin{PathPrefix: []string{testProfile + "/bin"}, System: "aarch64-darwin", ProfilePath: testProfile}
}

func TestComposeFontsListsTheMacsFoldersAndTheNewestMobileAsset(t *testing.T) {
	f := ComposeFonts(fontDeps(), fontDarwin(), testHostHome, false, false)
	want := []string{"/System/Library/Fonts", "/Library/Fonts",
		"/System/Library/AssetsV2/com_apple_MobileAsset_Font10"}
	if strings.Join(f.SystemDirs, ",") != strings.Join(want, ",") {
		t.Errorf("SystemDirs = %v, want %v (Font10 is newer than Font9, by number)", f.SystemDirs, want)
	}
	if strings.Join(f.ExtraReads, ",") != "/private/var/MobileAsset/AssetsV2/com_apple_MobileAsset_Font10" {
		t.Errorf("ExtraReads = %v, want the MobileAsset folder's real path", f.ExtraReads)
	}
	if f.UserFonts != "" || f.SharedFrom != "" {
		t.Errorf("the account mode without share_host_fonts lists your fonts: %+v", f)
	}
	conf := fontsConfContent(f)
	for _, w := range []string{"<dir>/System/Library/Fonts</dir>", "<dir>/Library/Fonts</dir>",
		"<dir>/System/Library/AssetsV2/com_apple_MobileAsset_Font10</dir>",
		"<dir>" + testProfile + "/share/fonts</dir>", `<cachedir prefix="xdg">fontconfig</cachedir>`,
		`<include ignore_missing="yes">` + testProfile + "/etc/fonts/conf.d</include>"} {
		if !strings.Contains(conf, w) {
			t.Errorf("fonts.conf lacks %s:\n%s", w, conf)
		}
	}
	if strings.Contains(conf, testHostHome) {
		t.Errorf("the account mode's fonts.conf names your home:\n%s", conf)
	}
	if got := ComposeFonts(fontDeps(), nil, testHostHome, true, true); got.Profile != "" {
		t.Errorf("no tool profile composed fonts: %+v", got)
	}
}

func TestComposeFontsReadsYourFontsInSelfModeAlways(t *testing.T) {
	user := testHostHome + "/Library/Fonts"
	f := ComposeFonts(fontDeps(user), fontDarwin(), testHostHome, true, false)
	if f.UserFonts != user || f.SharedFrom != "" {
		t.Errorf("self mode: %+v; want your folder itself listed, with no copy and no opt-in", f)
	}
	if !strings.Contains(fontsConfContent(f), "<dir>"+user+"</dir>") {
		t.Errorf("self mode's fonts.conf does not list %s", user)
	}
	if g := ComposeFonts(fontDeps(), fontDarwin(), testHostHome, true, false); g.UserFonts != "" || g.SharedMissing != "" {
		t.Errorf("a missing ~/Library/Fonts in self mode: %+v; want nothing listed or reported", g)
	}
}

// A run-as-you ~/Library/Fonts that is a link to elsewhere is not listed: the profile allow names
// the literal path.
func TestComposeFontsSkipsALinkedFontFolderInSelfMode(t *testing.T) {
	user := testHostHome + "/Library/Fonts"
	d := fontDeps(user)
	d.EvalSymlinks = func(p string) (string, bool) {
		if p == user {
			return "/Volumes/Fonts", true
		}
		return p, true
	}
	f := ComposeFonts(d, fontDarwin(), testHostHome, true, false)
	if f.UserFonts != "" || f.UserFontsLinked != user {
		t.Errorf("a linked ~/Library/Fonts: %+v; want it skipped and named", f)
	}
	if strings.Contains(fontsConfContent(f), user) || slices.Contains(f.SeatbeltReads(), user) {
		t.Errorf("a linked ~/Library/Fonts is listed or opened: %v", f.SeatbeltReads())
	}
	var out bytes.Buffer
	printFonts(printer{w: &out}, f, "/x.fonts.conf", "/u/config.jsonc")
	if !strings.Contains(out.String(), user+" is a link to another folder") {
		t.Errorf("the skip is not said:\n%s", out.String())
	}
}

// THE SEPARATE ACCOUNT reads a yolo-owned COPY of your fonts, never the folder in your home, and
// the opt-in off removes the copy.
func TestComposeFontsSharesACopyWithTheAccountOnlyWhenAsked(t *testing.T) {
	user := testHostHome + "/Library/Fonts"
	d := fontDeps(user)
	var staged, removed []string
	stage := d.StageFontCopy
	d.StageFontCopy = func(src, root string) FontCopy { staged = append(staged, src+" -> "+root); return stage(src, root) }
	d.RemoveFontCopy = func(root string) { removed = append(removed, root) }

	f := ComposeFonts(d, fontDarwin(), testHostHome, false, true)
	if f.SharedFrom != user || f.UserFonts != testFontCopy() {
		t.Fatalf("share_host_fonts: %+v; want the copy listed, from %s", f, user)
	}
	if fmt.Sprint(staged) != "["+user+" -> "+fontCopyRoot()+"]" || len(removed) != 0 {
		t.Errorf("staged %v, removed %v; want one refresh from your folder into %s", staged, removed, fontCopyRoot())
	}
	conf := fontsConfContent(f)
	if strings.Contains(conf, "<dir>"+user+"</dir>") || !strings.Contains(conf, "<dir>"+testFontCopy()+"</dir>") {
		t.Errorf("the account's fonts.conf names your folder rather than the copy:\n%s", conf)
	}
	for _, r := range f.SeatbeltReads() {
		if strings.HasPrefix(r, testHostHome) {
			t.Errorf("the account profile would open %s inside your home", r)
		}
	}

	staged, removed = nil, nil
	if g := ComposeFonts(d, fontDarwin(), testHostHome, false, false); g.UserFonts != "" ||
		fmt.Sprint(removed) != "["+fontCopyRoot()+"]" || len(staged) != 0 {
		t.Errorf("share_host_fonts off: %+v, staged %v, removed %v; want the copy removed", g, staged, removed)
	}
	removed = nil
	missing := ComposeFonts(fontDeps(), fontDarwin(), testHostHome, false, true)
	if missing.SharedMissing != user || missing.UserFonts != "" {
		t.Errorf("share_host_fonts with no folder: %+v; want it reported missing", missing)
	}
	// Self mode never copies and never removes another mode's copy by itself... it reads the folder.
	staged = nil
	if ComposeFonts(d, fontDarwin(), testHostHome, true, true); len(staged) != 0 {
		t.Errorf("self mode copied your fonts: %v", staged)
	}
}

// The copy's own refusals are each a line naming the next step.
func TestPrintFontsNamesWhatTheCopyLeftOut(t *testing.T) {
	user := testHostHome + "/Library/Fonts"
	base := FontSetup{Profile: testProfile, SharedFrom: user}
	cases := map[string]struct {
		copy FontCopy
		want []string
	}{
		"copied":  {FontCopy{Dir: testFontCopy()}, []string{"Sharing your fonts read-only (share_host_fonts): a copy of " + user + " at " + testFontCopy() + ", readable by _yolojail."}},
		"link":    {FontCopy{SourceIsLink: true}, []string{user + " is a symbolic link, so it is not shared", "Replace the link with the folder"}},
		"failed":  {FontCopy{Err: "disk full"}, []string{"could not copy " + user, "disk full", "Launch again"}},
		"partial": {FontCopy{Dir: testFontCopy(), Unreadable: []string{"a.ttf", "b.otf"}, Links: 1}, []string{"2 file(s) in " + user + " could not be read, so they are not shared: a.ttf, b.otf", "1 symbolic link(s)"}},
	}
	for name, c := range cases {
		f := base
		f.Copy, f.UserFonts = c.copy, c.copy.Dir
		var out bytes.Buffer
		printFonts(printer{w: &out}, f, "/x.fonts.conf", "/u/config.jsonc")
		for _, w := range c.want {
			if !strings.Contains(out.String(), w) {
				t.Errorf("%s: the launch does not say %q:\n%s", name, w, out.String())
			}
		}
	}
}

// THE PLAN: FONTCONFIG_FILE names the session's fonts.conf, which is swept with the env file;
// the account profile opens the copy of your fonts only with share_host_fonts, and nothing is
// granted inside your home.
func TestThePlanNamesTheFontsConfAndOpensYourFontsInTheAccountProfile(t *testing.T) {
	user := testHostHome + "/Library/Fonts"
	fonts := ComposeFonts(fontDeps(user), fontDarwin(), testHostHome, false, true)
	plan := BuildRunPlanWithDaemons("/Users/Shared/yolo/proj", jsonx.NewOrderedMap(), []string{"claude"},
		[]string{"claude"}, "/opt/yolo/bin/yolo", "", HomeOverlay{}, HostContext{}, jsonx.NewOrderedMap(),
		mockDarwin(), nil, JailDaemons{}, FloorStage{}, PlanSession{ID: "0123456789abcdef", Fonts: fonts})
	if problems := PlanInvariants(plan); len(problems) > 0 {
		t.Fatalf("invariants:\n%s", strings.Join(problems, "\n"))
	}
	key := SessionKey(plan.Cname, plan.SessionID)
	if plan.FontsConfFile != FontsConfFile(key, "") {
		t.Fatalf("fonts.conf %q is not the session's", plan.FontsConfFile)
	}
	if !SandboxEnvFileSets(plan.EnvFileContent, FontconfigFileVar, plan.FontsConfFile) ||
		!SandboxEnvFileSets(plan.EnvFileContent, FontconfigPathVar, pathParent(plan.FontsConfFile)) {
		t.Errorf("the env file does not name the fonts.conf:\n%s", plan.EnvFileContent)
	}
	if plan.FontsConfContent != fontsConfContent(fonts) {
		t.Errorf("the plan writes other bytes than it composed")
	}
	if !removesFile(plan.EnvFileRemoveCommands, plan.FontsConfFile) {
		t.Errorf("the fonts.conf is not swept with the env file")
	}
	if !strings.Contains(plan.Seatbelt, "#seatbelt-test-id:font-folder-read-allow#") ||
		!strings.Contains(plan.Seatbelt, "(subpath "+sbplStr(testFontCopy())+")") {
		t.Errorf("the account profile does not open the copy %s:\n%s", testFontCopy(), plan.Seatbelt)
	}
	if strings.Contains(plan.Seatbelt, sbplStr(user)) {
		t.Errorf("the account profile names your own font folder %s", user)
	}
	// After the /Users read deny it re-opens, or the deny would win.
	if strings.Index(plan.Seatbelt, "font-folder-read-allow") < strings.Index(plan.Seatbelt, "users-read-deny") {
		t.Errorf("the font allow precedes the /Users deny it must re-open")
	}
	if stage := fmt.Sprint(plan.StageCommands); strings.Contains(stage, testHostHome) {
		t.Errorf("the stage touches your home:\n%s", stage)
	}
	files := plan.fontsFilePlans()
	if len(files) != 1 || len(files[0].grant) != 1 {
		t.Errorf("fonts.conf install plan = %+v; want one file with the sandbox's read ACE", files)
	}

	// Without the opt-in: the file and its variable, and no font folder of yours.
	plain := BuildRunPlanWithDaemons("/Users/Shared/yolo/proj", jsonx.NewOrderedMap(), []string{"claude"},
		[]string{"claude"}, "/opt/yolo/bin/yolo", "", HomeOverlay{}, HostContext{}, jsonx.NewOrderedMap(),
		mockDarwin(), nil, JailDaemons{}, FloorStage{}, PlanSession{ID: "0123456789abcdef",
			Fonts: ComposeFonts(fontDeps(user), fontDarwin(), testHostHome, false, false)})
	if strings.Contains(plain.Seatbelt, sbplStr(user)) || strings.Contains(fmt.Sprint(plain.StageCommands), user) {
		t.Errorf("without share_host_fonts the account plan opens or grants %s", user)
	}
	if plain.FontsConfFile == "" {
		t.Errorf("without share_host_fonts there is no fonts.conf; the core row fires every launch")
	}
}

// A FONTCONFIG_FILE the user's own layers set wins, and the session's file is not written.
func TestAUserFontconfigFileWins(t *testing.T) {
	env := jsonx.NewOrderedMap()
	env.Set(FontconfigFileVar, "/Users/Shared/my-fonts.conf")
	plan := BuildRunPlanWithDaemons("/Users/Shared/yolo/proj", jsonx.NewOrderedMap(), []string{"claude"},
		[]string{"claude"}, "/opt/yolo/bin/yolo", "", HomeOverlay{}, HostContext{}, env,
		mockDarwin(), nil, JailDaemons{}, FloorStage{}, PlanSession{ID: "0123456789abcdef",
			Fonts: ComposeFonts(fontDeps(), fontDarwin(), testHostHome, false, false)})
	if plan.FontsConfFile != "" || !SandboxEnvFileSets(plan.EnvFileContent, FontconfigFileVar, "/Users/Shared/my-fonts.conf") {
		t.Errorf("the user's FONTCONFIG_FILE did not win: file %q\n%s", plan.FontsConfFile, plan.EnvFileContent)
	}
}

// THE RUN-AS-YOU PROFILE opens ~/Library/Fonts, exactly, and the session's fonts.conf; no grant.
func TestTheSelfProfileOpensYourFontsAndTheFontsConf(t *testing.T) {
	user := testHostHome + "/Library/Fonts"
	plan := BuildRunPlanWithDaemons("/Users/you/code/alpha", jsonx.NewOrderedMap(), nil, []string{"claude"},
		"/usr/local/bin/yolo", "", HomeOverlay{}, HostContext{}, jsonx.NewOrderedMap(), mockDarwin(), nil,
		JailDaemons{}, FloorStage{}, PlanSession{Mode: ModeSelf, HostHome: testHostHome, HostUser: "you",
			DarwinUserTempDir: "/private/var/folders/ab/xyz/T/", ID: "0123456789abcdef",
			Fonts: ComposeFonts(fontDeps(user), fontDarwin(), testHostHome, true, false)})
	if problems := PlanInvariants(plan); len(problems) > 0 {
		t.Fatalf("invariants:\n%s", strings.Join(problems, "\n"))
	}
	if !strings.Contains(plan.Seatbelt, "#seatbelt-test-id:font-folder-read-allow#") ||
		!strings.Contains(plan.Seatbelt, "(subpath "+sbplStr(user)+")") {
		t.Errorf("the self profile does not open %s", user)
	}
	if strings.Contains(plan.Seatbelt, "(subpath "+sbplStr(testHostHome+"/Library")+")") {
		t.Errorf("the self profile opens all of ~/Library, not ~/Library/Fonts alone")
	}
	if !strings.Contains(plan.Seatbelt, plan.FontsConfFile) || plan.FontsConfFile == "" {
		t.Errorf("the self profile does not read the session's fonts.conf %q", plan.FontsConfFile)
	}
	if f := plan.fontsFilePlans(); len(f) != 1 || len(f[0].grant) != 0 {
		t.Errorf("self mode grants access on its fonts.conf: %+v", f)
	}
}

// THE LAUNCH'S CALL SITES, through RunMacosUser: the fonts.conf is written after the env file and
// removed when the session ends, and the launch names your shared font folder.
func TestALaunchWritesTheFontsConfAndNamesYourSharedFonts(t *testing.T) {
	user := testHostHome + "/Library/Fonts"
	var rec []string
	d := caDeps(t, &rec, newKeychainFixture(t))
	base, orig := fontDeps(), d.PathIsDir
	fontsThere := true
	d.PathIsDir = func(p string) bool {
		if p == user {
			return fontsThere
		}
		return orig(p)
	}
	d.Glob, d.EvalSymlinks, d.StageFontCopy = base.Glob, base.EvalSymlinks, base.StageFontCopy
	var removedCopy []string
	d.RemoveFontCopy = func(root string) { removedCopy = append(removedCopy, root) }
	contents := map[string]string{}
	d.WriteHostFile = func(path, content string, mode os.FileMode) bool {
		rec = append(rec, fmt.Sprintf("write:%s %#o", path, mode))
		contents[path] = content
		return true
	}
	var out bytes.Buffer
	d.Out = &out
	o := newOpts("/Users/Shared/yolo/proj")
	o.HostHome, o.ShareHostFonts = testHostHome, true
	if rc := RunMacosUser(d, o); rc != 42 {
		t.Fatalf("rc = %d\n%s", rc, out.String())
	}
	var key string
	for _, r := range rec {
		if rest, ok := strings.CutPrefix(r, "write:"+stateDir+"/profile-"); ok {
			key = strings.TrimSuffix(strings.Fields(rest)[0], ".sb")
		}
	}
	file := FontsConfFile(key, "")
	joined := strings.Join(rec, "\n")
	if !strings.Contains(contents[file], "<dir>"+testFontCopy()+"</dir>") {
		t.Errorf("the launch did not write a fonts.conf listing the copy %s:\n%s", testFontCopy(), joined)
	}
	if strings.Contains(joined, testHostHome) && strings.Contains(joined, "chmod") &&
		strings.Contains(joined, "+a user:"+SandboxUser+" allow search "+testHostHome) {
		t.Errorf("the launch grants the account access inside your home:\n%s", joined)
	}
	if !SandboxEnvFileSets(contents[SandboxEnvFile(key, "")], FontconfigFileVar, file) {
		t.Errorf("the env file the launch wrote does not name %s", file)
	}
	if !strings.Contains(joined, "run:"+rmBin+" -f "+file) {
		t.Errorf("%s is not removed when the session ends", file)
	}
	disclosure := "Sharing your fonts read-only (share_host_fonts): a copy of " + user + " at " +
		testFontCopy() + ", readable by " + SandboxUser + "."
	if !strings.Contains(out.String(), disclosure) {
		t.Errorf("the launch does not say %q:\n%s", disclosure, out.String())
	}

	// A missing folder: one launch line naming it and the next step, and the old copy removed.
	out.Reset()
	rec = nil
	fontsThere = false
	if rc := RunMacosUser(d, o); rc != 42 {
		t.Fatalf("rc = %d\n%s", rc, out.String())
	}
	if !strings.Contains(out.String(), "share_host_fonts: "+user+" does not exist") ||
		fmt.Sprint(removedCopy) != "["+fontCopyRoot()+"]" {
		t.Errorf("a missing font folder was not reported, or its copy kept (%v):\n%s", removedCopy, out.String())
	}
}
