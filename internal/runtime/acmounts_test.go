package runtime

import (
	"slices"
	"testing"
)

// acInspectWithMounts is acInspectMeasured's shape with one mount entry. The entry's field
// names are Apple Container's Filesystem fields, NOT measured on a Mac (see
// MountsFromContainerInspectJSON).
const acInspectWithMounts = `[{"id":"yolo-ws-abcd1234","status":{"state":"running"},
 "configuration":{"mounts":[{"type":{"virtiofs":{}},"source":"/Users/me/.local/share/yolo-jail/flake-bundles/x/bin/linux-arm64","destination":"/opt/yolo-jail/bin","options":["ro"]}],
  "initProcess":{"environment":["PATH=/bin"]}}}]`

func TestMountsFromContainerInspectJSON(t *testing.T) {
	for _, tc := range []struct {
		name, payload string
		want          []InspectMount
		ok            bool
	}{
		{"one mount", acInspectWithMounts, []InspectMount{{
			Source: "/Users/me/.local/share/yolo-jail/flake-bundles/x/bin/linux-arm64", Destination: "/opt/yolo-jail/bin"}}, true},
		{"the measured payload, no mounts", acInspectMeasured, []InspectMount{}, true},
		{"a bare object", `{"configuration":{"mounts":[{"Source":"/a","Destination":"/b"}]}}`, []InspectMount{{"/a", "/b"}}, true},
		{"no mounts key", `[{"configuration":{}}]`, nil, false},
		{"mounts null", `[{"configuration":{"mounts":null}}]`, nil, false},
		{"an entry with no destination", `[{"configuration":{"mounts":[{"source":"/a"}]}}]`, nil, false},
		{"an empty array", `[]`, nil, false},
		{"not JSON", "x", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := MountsFromContainerInspectJSON(tc.payload)
			if ok != tc.ok || !slices.Equal(got, tc.want) {
				t.Errorf("MountsFromContainerInspectJSON = %v, %v; want %v, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}
