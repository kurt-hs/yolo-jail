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

func TestPackagesLaunchKeySeparatesAmbientBytesAttributeAndSystem(t *testing.T) {
	base := AutoLoadOptions{AmbientExtraPackages: `[{"name":"hello"}]`}
	key := base.packagesLaunchKey(testIdentity)
	if key == "" {
		t.Fatal("ambient bytes did not produce a key")
	}
	for name, changed := range map[string]AutoLoadOptions{
		"ambient bytes": {AmbientExtraPackages: `[{ "name":"hello" }]`},
		"lean attr":     {AmbientExtraPackages: base.AmbientExtraPackages, Attr: ImageAttrLean},
	} {
		if got := changed.packagesLaunchKey(testIdentity); got == "" || got == key {
			t.Errorf("%s did not isolate key: %q", name, got)
		}
	}
	if got := base.packagesLaunchKey(otherIdentity); got == key {
		t.Fatal("changed identity reused key")
	}
	if got := base.packagesLaunchKeyForSystem(testIdentity, "aarch64-linux"); got == key {
		t.Fatal("changed system reused key")
	}
	unknown := AutoLoadOptions{Attr: "unknown"}
	if got := unknown.packagesLaunchKey(testIdentity); got != "" {
		t.Fatalf("unknown attr accepted: %s", got)
	}
	moving := AutoLoadOptions{AmbientExtraPackages: `[{"nixpkgs":"unstable"}]`}
	if got := moving.packagesLaunchKey(testIdentity); got != "" {
		t.Fatalf("ambient moving pin accepted: %s", got)
	}
}

func TestAmbientPackagesLaunchReusesExactEnvironmentBytes(t *testing.T) {
	withBuildDir(t)
	var out bytes.Buffer
	f := newFakeRuntime()
	o := stockOpts(t, f, &out, testIdentity)
	o.AmbientExtraPackages = `[{"name":"hello"}]`
	builds := 0
	build := o.BuildStorePath
	o.BuildStorePath = func(r string, p []any, l string) (string, []string) { builds++; return build(r, p, l) }
	first := AutoLoadImage(o)
	second := AutoLoadImage(o)
	if !first.OK || !second.OK || builds != 1 || first.StorePath != second.StorePath {
		t.Fatalf("first=%+v second=%+v builds=%d\n%s", first, second, builds, out.String())
	}
	firstKey := o.packagesLaunchKey(testIdentity)
	o.AmbientExtraPackages = `[{ "name":"hello" }]`
	if changedKey := o.packagesLaunchKey(testIdentity); changedKey == firstKey {
		t.Fatal("changed ambient bytes retained the record key")
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

func TestPackagesLaunchKeyRejectsUnsupportedPackageObjects(t *testing.T) {
	o := AutoLoadOptions{ExtraPackages: []any{map[string]string{"name": "hello", "nixpkgs": strings.Repeat("a", 40)}}}
	if key := o.packagesLaunchKey(testIdentity); key != "" {
		t.Fatalf("unsupported package object representation acquired a key: %s", key)
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
