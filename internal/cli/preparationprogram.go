package cli

import (
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
)

// admittedProgramServing qualifies the selected program's exact Good.Entry, never a
// scheduler snapshot or an independently recovered older build. It does not repair
// records, advance sources, materialize programs, or consume background notices.
func admittedProgramServing(f packload.Fork, runtime, platform string) (entrypoint.ForkDelivery, *packsrc.GoodBuild) {
	refuse := func(why string) (entrypoint.ForkDelivery, *packsrc.GoodBuild) {
		return entrypoint.ForkDelivery{Reason: fmt.Sprintf("%s: %s — run `yolo host apply --assert` on a supported container host to prepare the selected program", f.Key(), why)}, nil
	}
	if runtime != "podman" && runtime != "container" {
		return refuse("this runtime does not deliver captured programs")
	}
	osname, arch, ok := strings.Cut(platform, "/")
	if !ok || osname != "linux" || (arch != "amd64" && arch != "arm64") {
		return refuse("unsupported capture platform " + platform)
	}
	if f.IsTree() || !f.Patched() {
		return refuse("not a patched program")
	}
	if why := (packdecl.Install{Platforms: f.Platforms}).UnpublishedReason(osname, arch); why != "" {
		return refuse(why)
	}
	selected := selectConfiguredHostPacks()
	if problems := selected.problems(); len(problems) > 0 {
		return refuse(fmt.Sprintf("user selection unavailable: %v", problems))
	}
	authorized := false
	for _, current := range packload.PatchedForks(selected.packs) {
		want, got := f, current
		want.Root = ""
		got.Root = ""
		if reflect.DeepEqual(want, got) {
			f = current
			authorized = true
			break
		}
	}
	if !authorized {
		return refuse("program declaration is no longer selected with these inputs")
	}
	a, early := newAdvance(f, advanceOptions{platform: platform, runtime: runtime, out: io.Discard, errw: io.Discard, host: true, readOnlyInitialization: true})
	if early != nil {
		return refuse(early.delivery.Reason)
	}
	// Read disk authority separately: newAdvance's read-only initialization may recover
	// in memory for ordinary cold readiness, which is not an admitted-read credential.
	record, err := a.packs.LoadCheckRecord(f.Key())
	if err != nil {
		return refuse("current Good record unavailable: " + err.Error())
	}
	if record.Owner != f.Key() || record.Good == nil || record.Good.Recipe != a.recipe || record.Good.Series != a.series.Digest || record.Good.Read == nil || *record.Good.Read != a.in {
		return refuse("no exact current Good recipe/series for this owner")
	}
	patch := a.deliveryPatchFailure(false)
	if patch.State == "unavailable" {
		return refuse("patch authority unavailable: " + patch.Diagnostic)
	}
	if patch.Failure != nil {
		return entrypoint.ForkDelivery{PatchFailure: patch.Failure, Reason: patch.Failure.Error()}, nil
	}
	if record.ApplyErrAtLastCheck() != nil {
		return refuse("earlier replay error needs explicit preparation before delivery")
	}
	good := *record.Good
	entry, _, why := validateCachedGood(f, platform, &good)
	if why != "" {
		return refuse(why)
	}
	if a.serving == nil || a.serving.Key != good.Entry || entry.Key != good.Entry {
		return refuse("current serving entry does not match exact Good.Entry")
	}
	return entrypoint.ForkDelivery{Key: entry.Key}, &good
}
