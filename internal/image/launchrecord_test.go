package image

import (
	"bytes"
	"strings"
	"testing"
)

func TestPackagesLaunchReusesItsRecordedImage(t *testing.T) {
	withBuildDir(t)
	var out bytes.Buffer
	f := newFakeRuntime()
	o := stockOpts(t, f, &out, testIdentity)
	o.ExtraPackages = []any{"hello"}
	builds := 0
	build := o.BuildStorePath
	o.BuildStorePath = func(r string, p []any, l string) (string, []string) { builds++; return build(r, p, l) }
	first := AutoLoadImage(o)
	second := AutoLoadImage(o)
	if !first.OK || !second.OK || builds != 1 || second.StorePath != first.StorePath || second.Ref != first.Ref {
		t.Fatalf("first=%+v second=%+v builds=%d\n%s", first, second, builds, out.String())
	}
	if !strings.Contains(out.String(), "matches this launch's image inputs") {
		t.Fatal(out.String())
	}
	o.ExtraPackages = []any{"git"}
	AutoLoadImage(o)
	if builds != 2 {
		t.Fatalf("changed packages reused old record: builds=%d", builds)
	}
}

func TestPackagesLaunchKeyRejectsMovingPins(t *testing.T) {
	o := AutoLoadOptions{ExtraPackages: []any{map[string]any{"name": "hello", "nixpkgs": "nixos-unstable"}}}
	if key := o.packagesLaunchKey(testIdentity); key != "" {
		t.Fatalf("moving pin accepted: %s", key)
	}
	o.ExtraPackages = []any{map[string]any{"name": "hello", "nixpkgs": strings.Repeat("a", 40)}}
	if key := o.packagesLaunchKey(testIdentity); key == "" {
		t.Fatal("full commit rejected")
	}
}

func TestPackagesLaunchDoesNotReuseAnUnprovenImage(t *testing.T) {
	for _, failure := range []string{"invalid store", "missing image", "changed identity", "moving pin", "lean attr", "failed identity eval"} {
		t.Run(failure, func(t *testing.T) {
			withBuildDir(t)
			var out bytes.Buffer
			f := newFakeRuntime()
			o := stockOpts(t, f, &out, testIdentity)
			o.ExtraPackages = []any{"hello"}
			builds := 0
			build := o.BuildStorePath
			o.BuildStorePath = func(r string, p []any, l string) (string, []string) { builds++; return build(r, p, l) }
			first := AutoLoadImage(o)
			if !first.OK {
				t.Fatal(out.String())
			}
			switch failure {
			case "invalid store":
				o.StorePathValid = func(string) bool { return false }
			case "missing image":
				o.Run = newFakeRuntime().run
			case "changed identity":
				o.EvalIdentity = func(string) (string, bool) { return otherIdentity, true }
			case "moving pin":
				o.ExtraPackages = []any{map[string]any{"name": "hello", "nixpkgs": "unstable"}}
			case "lean attr":
				o.Attr = ImageAttrLean
			case "failed identity eval":
				o.EvalIdentity = func(string) (string, bool) { return testIdentity, false }
			}
			AutoLoadImage(o)
			if builds != 2 {
				t.Fatalf("unproven record skipped build: %d", builds)
			}
		})
	}
}
