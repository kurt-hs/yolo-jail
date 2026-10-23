package run

// packprogramneed_test.go pins the launch's half of OQ-PS5 and PS-D18
// (docs/design/provisioner-sets.md) through the real stagePacks, so deleting the pre-flight or
// the provided-program line fails here rather than only in packload's unit tests.

import (
	"strings"
	"testing"
)

func TestLaunchNamesThePackThatProvidesANeed(t *testing.T) {
	o := twoLocalPacks(t,
		"needer", `{"contributes":[{"kind":"program","bin":"xtool"}]}`,
		"provider", `{"contributes":[{"kind":"program","bin":"xtool","via":"npm","package":"xtool"}]}`)
	var stderr strings.Builder
	o.Stderr = &stderr
	if _, _, _, err := o.stagePacks("yolo-test-programneed"); err != nil {
		t.Fatalf("a need another selected pack provides must launch; got: %v", err)
	}
	if want := "program xtool, needed by pack needer, is provided by pack provider"; !strings.Contains(stderr.String(), want) {
		t.Errorf("the launch must say where the program came from (%q); stderr:\n%s", want, stderr.String())
	}
}

func TestLaunchRefusesTwoProvidersOfOneProgram(t *testing.T) {
	o := twoLocalPacks(t,
		"bravo", `{"contributes":[{"kind":"program","bin":"xtool","via":"npm","package":"xtool"}]}`,
		"charlie", `{"contributes":[{"kind":"program","bin":"xtool","via":"installer","url":"https://example.com/x.sh"}]}`)
	_, _, _, err := o.stagePacks("yolo-test-programprovider")
	if err == nil {
		t.Fatal("two selected packs carrying a recipe for one program must refuse the launch (PS-D18)")
	}
	for _, want := range []string{"program xtool provided by bravo, charlie", "pack bravo", "pack charlie", "from `packs`"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal missing %q; got:\n%s", want, err)
		}
	}
}
