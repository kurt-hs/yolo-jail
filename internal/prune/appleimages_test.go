package prune

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

const appleDigestA = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const appleDigestB = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func appleImage(ref, digest string) map[string]any {
	return map[string]any{
		"id": digest[len("sha256:"):],
		"configuration": map[string]any{
			"name":       ref,
			"descriptor": map[string]any{"digest": digest},
		},
		"variants": []any{},
	}
}

func appleContainer(ref, digest string) map[string]any {
	return map[string]any{
		"id": "foreign-container",
		"configuration": map[string]any{
			"image": map[string]any{
				"reference":  ref,
				"descriptor": map[string]any{"digest": digest},
			},
		},
		"status": map[string]any{"state": "running"},
	}
}

func appleJSON(t *testing.T, value any) string {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func appleImageInspectResponse(t *testing.T, images, ref string) string {
	t.Helper()
	var rows []map[string]any
	if err := json.Unmarshal([]byte(images), &rows); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		configuration, _ := row["configuration"].(map[string]any)
		if configuration["name"] == ref {
			return appleJSON(t, []any{row})
		}
	}
	return "[]"
}

func appleProbeRunner(t *testing.T, images, containers string, imageRC, containerRC int) (RunFunc, *[][]string) {
	t.Helper()
	var calls [][]string
	run := func(argv []string, _ time.Duration) ProbeResult {
		calls = append(calls, append([]string(nil), argv...))
		switch {
		case reflect.DeepEqual(argv, []string{"container", "image", "list", "--format", "json"}):
			return ProbeResult{Stdout: images, RC: imageRC, Ran: true}
		case reflect.DeepEqual(argv, []string{"container", "ls", "--all", "--format", "json"}):
			return ProbeResult{Stdout: containers, RC: containerRC, Ran: true}
		case len(argv) == 4 && argv[0] == "container" && argv[1] == "image" && argv[2] == "inspect":
			return ProbeResult{Stdout: appleImageInspectResponse(t, images, argv[3]), Ran: true}
		default:
			if len(argv) == 4 && argv[0] == "container" && argv[1] == "image" && argv[2] == "delete" {
				return ProbeResult{Ran: true}
			}
			return ProbeResult{RC: 1, Ran: true}
		}
	}
	return run, &calls
}

// These source-derived fixtures pin the production selector to Apple's JSON
// protocol and deletion grammar; they are not captured native-runtime output.
func TestPruneOldImagesAppleEmptyListing(t *testing.T) {
	run, calls := appleProbeRunner(t, "[]", "[]", 0, 0)
	removed, declined := PruneOldImages("container", nil, true, true, run)
	if len(removed) != 0 || declined != "" {
		t.Fatalf("empty Apple store: removed=%v declined=%q calls=%v; want known empty and no deletion", removed, declined, *calls)
	}
	if !reflect.DeepEqual(*calls, [][]string{{"container", "image", "list", "--format", "json"}}) {
		t.Fatalf("empty listing calls=%v; want only native image-list command", *calls)
	}
}

func TestPruneOldImagesAppleUsesSupportedJSONAndDeleteReference(t *testing.T) {
	images := appleJSON(t, []any{appleImage("yolo-jail:old", appleDigestA)})
	run, calls := appleProbeRunner(t, images, "[]", 0, 0)
	removed, declined := PruneOldImages("container", nil, true, true, run)
	want := [][]string{
		{"container", "image", "list", "--format", "json"},
		{"container", "ls", "--all", "--format", "json"},
		{"container", "image", "list", "--format", "json"},
		{"container", "ls", "--all", "--format", "json"},
		{"container", "image", "inspect", "yolo-jail:old"},
		{"container", "image", "delete", "yolo-jail:old"},
	}
	if declined != "" || !reflect.DeepEqual(removed, []string{"yolo-jail:old"}) || !reflect.DeepEqual(*calls, want) {
		t.Fatalf("removed=%v declined=%q calls=%v; want exact native list/liveness/delete sequence %v", removed, declined, *calls, want)
	}
}

func TestPruneOldImagesAppleFailedDeleteIsNotReported(t *testing.T) {
	images := appleJSON(t, []any{appleImage("yolo-jail:old", appleDigestA)})
	var deletes int
	run := func(argv []string, _ time.Duration) ProbeResult {
		if reflect.DeepEqual(argv, []string{"container", "image", "list", "--format", "json"}) {
			return ProbeResult{Stdout: images, Ran: true}
		}
		if reflect.DeepEqual(argv, []string{"container", "ls", "--all", "--format", "json"}) {
			return ProbeResult{Stdout: "[]", Ran: true}
		}
		if reflect.DeepEqual(argv, []string{"container", "image", "inspect", "yolo-jail:old"}) {
			return ProbeResult{Stdout: appleImageInspectResponse(t, images, "yolo-jail:old"), Ran: true}
		}
		if reflect.DeepEqual(argv, []string{"container", "image", "delete", "yolo-jail:old"}) {
			deletes++
			return ProbeResult{RC: 1, Ran: true}
		}
		return ProbeResult{RC: 1, Ran: true}
	}
	removed, declined := PruneOldImages("container", nil, true, true, run)
	if declined != "" || len(removed) != 0 || deletes != 1 {
		t.Fatalf("failed delete reported removed=%v declined=%q deletes=%d", removed, declined, deletes)
	}
}

func TestPruneOldImagesAppleRejectsInspectAliasResolution(t *testing.T) {
	images := appleJSON(t, []any{appleImage("yolo-jail:old", appleDigestA)})
	var deletes int
	run := func(argv []string, _ time.Duration) ProbeResult {
		if reflect.DeepEqual(argv, []string{"container", "image", "list", "--format", "json"}) {
			return ProbeResult{Stdout: images, Ran: true}
		}
		if reflect.DeepEqual(argv, []string{"container", "ls", "--all", "--format", "json"}) {
			return ProbeResult{Stdout: "[]", Ran: true}
		}
		if reflect.DeepEqual(argv, []string{"container", "image", "inspect", "yolo-jail:old"}) {
			return ProbeResult{Stdout: appleJSON(t, []any{appleImage("yolo-jail:elsewhere", appleDigestB)}), Ran: true}
		}
		if len(argv) == 4 && argv[0] == "container" && argv[1] == "image" && argv[2] == "delete" {
			deletes++
			return ProbeResult{Ran: true}
		}
		return ProbeResult{RC: 1, Ran: true}
	}
	removed, declined := PruneOldImages("container", nil, true, true, run)
	if declined != "" || len(removed) != 0 || deletes != 0 {
		t.Fatalf("inspect alias resolution must skip deletion: removed=%v declined=%q deletes=%d", removed, declined, deletes)
	}
}

func TestPruneOldImagesAppleFreshAliasBindingSkipsDelete(t *testing.T) {
	initial := appleJSON(t, []any{appleImage("yolo-jail:old", appleDigestA)})
	fresh := appleJSON(t, []any{
		appleImage("yolo-jail:old", appleDigestA),
		appleImage("docker.io/library/yolo-jail:current", appleDigestA),
	})
	var listings, deletes int
	run := func(argv []string, _ time.Duration) ProbeResult {
		if reflect.DeepEqual(argv, []string{"container", "image", "list", "--format", "json"}) {
			listings++
			if listings == 1 {
				return ProbeResult{Stdout: initial, Ran: true}
			}
			return ProbeResult{Stdout: fresh, Ran: true}
		}
		if reflect.DeepEqual(argv, []string{"container", "ls", "--all", "--format", "json"}) {
			return ProbeResult{Stdout: "[]", Ran: true}
		}
		if len(argv) == 4 && argv[0] == "container" && argv[1] == "image" && argv[2] == "delete" {
			deletes++
			return ProbeResult{Ran: true}
		}
		return ProbeResult{RC: 1, Ran: true}
	}
	removed, declined := PruneOldImages("container", map[string]struct{}{"current": {}}, true, true, run)
	if declined != "" || len(removed) != 0 || deletes != 0 {
		t.Fatalf("new protected alias did not save image: removed=%v declined=%q deletes=%d", removed, declined, deletes)
	}
}

func TestPruneOldImagesAppleDeclinesUnknownInventoryOrLiveness(t *testing.T) {
	for _, tc := range []struct {
		name         string
		images       string
		containers   string
		imageRC      int
		containerRC  int
		wantDeclined ImageReapDecline
	}{
		{name: "failed image list", images: "[]", containers: "[]", imageRC: 1, wantDeclined: DeclineImagesUnreadable},
		{name: "malformed image json", images: "{", containers: "[]", wantDeclined: DeclineImagesUnreadable},
		{name: "null image list", images: "null", containers: "[]", wantDeclined: DeclineImagesUnreadable},
		{name: "missing image identity", images: `[{"configuration":{"name":"yolo-jail:old"}}]`, containers: "[]", wantDeclined: DeclineImagesUnreadable},
		{name: "failed liveness", images: appleJSON(t, []any{appleImage("yolo-jail:old", appleDigestA)}), containers: "[]", containerRC: 1, wantDeclined: DeclineRuntimeUnreachable},
		{name: "malformed liveness", images: appleJSON(t, []any{appleImage("yolo-jail:old", appleDigestA)}), containers: "{}", wantDeclined: DeclineRuntimeUnreachable},
		{name: "missing liveness identity", images: appleJSON(t, []any{appleImage("yolo-jail:old", appleDigestA)}), containers: `[{"configuration":{"image":{"reference":"yolo-jail:old"}}}]`, wantDeclined: DeclineRuntimeUnreachable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run, calls := appleProbeRunner(t, tc.images, tc.containers, tc.imageRC, tc.containerRC)
			removed, declined := PruneOldImages("container", nil, true, true, run)
			if len(removed) != 0 || declined != tc.wantDeclined {
				t.Fatalf("removed=%v declined=%q; want no removal and %q; calls=%v", removed, declined, tc.wantDeclined, *calls)
			}
			for _, argv := range *calls {
				if len(argv) > 3 && argv[1] == "image" && argv[2] == "delete" {
					t.Fatalf("declined sweep deleted image: %v", argv)
				}
			}
		})
	}
}

func TestPruneOldImagesAppleProtectsAliasesCurrentAndInUse(t *testing.T) {
	images := appleJSON(t, []any{
		appleImage("yolo-jail:protected", appleDigestA),
		appleImage("docker.io/library/yolo-jail:alias", appleDigestA),
		appleImage("yolo-jail:running", appleDigestB),
		appleImage("yolo-jail:old", "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"),
	})
	containers := appleJSON(t, []any{appleContainer("yolo-jail:running", appleDigestB)})
	run, calls := appleProbeRunner(t, images, containers, 0, 0)
	removed, declined := PruneOldImages("container", map[string]struct{}{"protected": {}}, true, true, run)
	if declined != "" || !reflect.DeepEqual(removed, []string{"yolo-jail:old"}) {
		t.Fatalf("removed=%v declined=%q calls=%v; want only unrelated stale image", removed, declined, *calls)
	}
}

func TestPruneOldImagesAppleRejectsAmbiguousRefsAndRepositoryLookalikes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		images any
		declin bool
	}{
		{name: "duplicate ref with different identity", images: []any{appleImage("yolo-jail:old", appleDigestA), appleImage("yolo-jail:old", appleDigestB)}, declin: true},
		{name: "repository lookalike ignored", images: []any{appleImage("yolo-jail-builder:old", appleDigestA)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run, calls := appleProbeRunner(t, appleJSON(t, tc.images), "[]", 0, 0)
			removed, declined := PruneOldImages("container", nil, true, true, run)
			if tc.declin {
				if len(removed) != 0 || declined != DeclineImagesUnreadable {
					t.Fatalf("ambiguous refs removed=%v declined=%q calls=%v", removed, declined, *calls)
				}
			} else if len(removed) != 0 || declined != "" {
				t.Fatalf("foreign repository removed=%v declined=%q calls=%v", removed, declined, *calls)
			}
		})
	}
}

func TestPruneOldImagesAppleDryRunAndOptionalWidening(t *testing.T) {
	images := appleJSON(t, []any{appleImage("yolo-jail:old", appleDigestA)})
	run, calls := appleProbeRunner(t, images, "[]", 0, 0)
	removed, declined := PruneOldImages("container", nil, true, false, run)
	want := [][]string{{"container", "image", "list", "--format", "json"}, {"container", "ls", "--all", "--format", "json"}}
	if declined != "" || !reflect.DeepEqual(removed, []string{"yolo-jail:old"}) || !reflect.DeepEqual(*calls, want) {
		t.Fatalf("dry-run removed=%v declined=%q calls=%v; want selection only, no unsupported optional label probe or delete", removed, declined, *calls)
	}
}

func TestPruneOldImagesAppleGuardRechecksNewCurrentAlias(t *testing.T) {
	initial := appleJSON(t, []any{appleImage("yolo-jail:old", appleDigestA)})
	fresh := appleJSON(t, []any{
		appleImage("yolo-jail:old", appleDigestA),
		appleImage("yolo-jail:current", appleDigestA),
	})
	var listings, deletes int
	run := func(argv []string, _ time.Duration) ProbeResult {
		if reflect.DeepEqual(argv, []string{"container", "image", "list", "--format", "json"}) {
			listings++
			if listings == 1 {
				return ProbeResult{Stdout: initial, Ran: true}
			}
			return ProbeResult{Stdout: fresh, Ran: true}
		}
		if reflect.DeepEqual(argv, []string{"container", "ls", "--all", "--format", "json"}) {
			return ProbeResult{Stdout: "[]", Ran: true}
		}
		if len(argv) == 4 && argv[0] == "container" && argv[1] == "image" && argv[2] == "inspect" {
			return ProbeResult{Stdout: appleImageInspectResponse(t, fresh, argv[3]), Ran: true}
		}
		if len(argv) == 4 && argv[0] == "container" && argv[1] == "image" && argv[2] == "delete" {
			deletes++
			return ProbeResult{Ran: true}
		}
		return ProbeResult{RC: 1, Ran: true}
	}
	guard := Guard(func(recheck func() bool, _ func()) bool {
		if recheck() {
			t.Fatal("new current alias passed guarded recheck")
		}
		return false
	})
	pointerRecheck := func(tags []string) bool {
		for _, tag := range tags {
			if tag == "current" {
				return false
			}
		}
		return true
	}
	removed, declined := pruneOldImages("container", nil, true, true, run, guard, pointerRecheck)
	if declined != "" || len(removed) != 0 || deletes != 0 {
		t.Fatalf("fresh current alias did not veto delete: removed=%v declined=%q deletes=%d", removed, declined, deletes)
	}
}

func TestPruneOldImagesAppleRechecksBindingInsideGuard(t *testing.T) {
	images := appleJSON(t, []any{appleImage("yolo-jail:old", appleDigestA)})
	containers := "[]"
	calls := 0
	run := func(argv []string, _ time.Duration) ProbeResult {
		if reflect.DeepEqual(argv, []string{"container", "image", "list", "--format", "json"}) {
			calls++
			return ProbeResult{Stdout: images, Ran: true}
		}
		if reflect.DeepEqual(argv, []string{"container", "ls", "--all", "--format", "json"}) {
			calls++
			return ProbeResult{Stdout: containers, Ran: true}
		}
		if reflect.DeepEqual(argv, []string{"container", "image", "delete", "yolo-jail:old"}) {
			calls++
			return ProbeResult{Ran: true}
		}
		return ProbeResult{RC: 1, Ran: true}
	}
	guard := Guard(func(recheck func() bool, del func()) bool {
		containers = appleJSON(t, []any{appleContainer("yolo-jail:old", appleDigestA)})
		if recheck() {
			t.Fatal("candidate that became in-use passed guarded recheck")
		}
		return false
	})
	removed, declined := pruneOldImages("container", nil, true, true, run, guard, nil)
	if declined != "" || len(removed) != 0 {
		t.Fatalf("in-use binding after selection must skip delete: removed=%v declined=%q calls=%d", removed, declined, calls)
	}
	if calls != 4 { // initial inventory + liveness + fresh guarded inventory + fresh guarded liveness
		t.Fatalf("guarded recheck made %d probes; want four and no deletion", calls)
	}
}

func TestPruneOldImagesAppleSkipsFailedImageInspect(t *testing.T) {
	images := appleJSON(t, []any{appleImage("yolo-jail:old", appleDigestA)})
	for _, tc := range []struct {
		name   string
		result ProbeResult
	}{
		{name: "could not run", result: ProbeResult{}},
		{name: "non-zero exit", result: ProbeResult{RC: 1, Ran: true}},
		{name: "malformed JSON", result: ProbeResult{Stdout: "{", Ran: true}},
		{name: "ambiguous array", result: ProbeResult{Stdout: appleJSON(t, []any{
			appleImage("yolo-jail:old", appleDigestA), appleImage("yolo-jail:other", appleDigestB),
		}), Ran: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var deletes int
			run := func(argv []string, _ time.Duration) ProbeResult {
				if reflect.DeepEqual(argv, []string{"container", "image", "list", "--format", "json"}) {
					return ProbeResult{Stdout: images, Ran: true}
				}
				if reflect.DeepEqual(argv, []string{"container", "ls", "--all", "--format", "json"}) {
					return ProbeResult{Stdout: "[]", Ran: true}
				}
				if reflect.DeepEqual(argv, []string{"container", "image", "inspect", "yolo-jail:old"}) {
					return tc.result
				}
				if len(argv) == 4 && argv[0] == "container" && argv[1] == "image" && argv[2] == "delete" {
					deletes++
					return ProbeResult{Ran: true}
				}
				return ProbeResult{RC: 1, Ran: true}
			}
			removed, declined := PruneOldImages("container", nil, true, true, run)
			if declined != "" || len(removed) != 0 || deletes != 0 {
				t.Fatalf("unknown inspect result must skip deletion: removed=%v declined=%q deletes=%d", removed, declined, deletes)
			}
		})
	}
}
