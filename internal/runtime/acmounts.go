package runtime

import "encoding/json"

// InspectMount is one bind mount a container reports: the host source and the
// container-side destination.
type InspectMount struct {
	Source, Destination string
}

// MountsFromContainerInspectJSON returns the mounts in Apple Container's `container inspect`
// output, the sibling of EnvFromContainerInspectJSON: that inspect takes no --format, so the
// whole document is read. The measured payload (docs/plans/setup-support-gaps.md §5.1 row 5) is
// a top-level array whose configuration.mounts is a real array; a single object is accepted too.
//
// ⚠ THE ELEMENT'S FIELD NAMES ARE NOT MEASURED. The measurement recorded that mounts is an
// array, not what one entry holds; `source` and `destination` are Apple Container's Filesystem
// field names, unverified on a Mac (disk-footprint.md BF-D4). The capitalized spellings podman
// uses are accepted as well.
//
// STRICT, because a destructive caller reads ok=false as "could not ask": ok is false when the
// output is not such a document, carries no configuration.mounts array, or holds an entry with
// no destination string. An empty array is an answer: a container with no mounts.
func MountsFromContainerInspectJSON(stdout string) ([]InspectMount, bool) {
	type doc struct {
		Configuration struct {
			Mounts *[]map[string]any `json:"mounts"`
		} `json:"configuration"`
	}
	var first doc
	var docs []doc
	if err := json.Unmarshal([]byte(stdout), &docs); err == nil {
		if len(docs) == 0 {
			return nil, false
		}
		first = docs[0]
	} else if err := json.Unmarshal([]byte(stdout), &first); err != nil {
		return nil, false
	}
	if first.Configuration.Mounts == nil {
		return nil, false
	}
	str := func(m map[string]any, keys ...string) (string, bool) {
		for _, k := range keys {
			if v, ok := m[k].(string); ok {
				return v, true
			}
		}
		return "", false
	}
	out := []InspectMount{}
	for _, m := range *first.Configuration.Mounts {
		if m == nil {
			return nil, false
		}
		dest, ok := str(m, "destination", "Destination")
		if !ok {
			return nil, false
		}
		src, _ := str(m, "source", "Source")
		out = append(out, InspectMount{Source: src, Destination: dest})
	}
	return out, true
}
