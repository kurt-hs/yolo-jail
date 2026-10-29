package prune

import (
	"encoding/json"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/image"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

type appleImageRecord struct {
	Reference string
	Digest    string
}

type appleImageJSON struct {
	Configuration *struct {
		Name       *string `json:"name"`
		Descriptor *struct {
			Digest *string `json:"digest"`
		} `json:"descriptor"`
	} `json:"configuration"`
}

type appleContainerJSON struct {
	Configuration *struct {
		Image *struct {
			Reference  *string `json:"reference"`
			Descriptor *struct {
				Digest *string `json:"digest"`
			} `json:"descriptor"`
		} `json:"image"`
	} `json:"configuration"`
}

func appleJSONArray(stdout string, dst any) bool {
	trimmed := strings.TrimSpace(stdout)
	if trimmed == "" || trimmed == "null" || !strings.HasPrefix(trimmed, "[") {
		return false
	}
	return json.Unmarshal([]byte(trimmed), dst) == nil
}

func validAppleImageDigest(digest string) bool {
	const prefix = "sha256:"
	if !strings.HasPrefix(digest, prefix) || len(digest) != len(prefix)+64 {
		return false
	}
	for _, c := range digest[len(prefix):] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func appleRepository(ref string) (string, string, bool) {
	colon := strings.LastIndexByte(ref, ':')
	slash := strings.LastIndexByte(ref, '/')
	if colon <= slash || colon == len(ref)-1 {
		return "", "", false
	}
	repo, tag := ref[:colon], ref[colon+1:]
	if repo == "" || strings.ContainsAny(tag, " \t\r\n") {
		return "", "", false
	}
	return repo, tag, true
}

func isAppleJailRepository(repo string) bool {
	return repo == paths.JailImageRepoShort || repo == paths.JailImageRepo || repo == "docker.io/library/"+paths.JailImageRepoShort
}

func readAppleImages(rt string, run RunFunc) ([]appleImageRecord, bool) {
	res := run([]string{rt, "image", "list", "--format", "json"}, psTimeout)
	if !res.Ran || res.RC != 0 {
		return nil, false
	}
	var rows []appleImageJSON
	if !appleJSONArray(res.Stdout, &rows) || rows == nil {
		return nil, false
	}
	images := make([]appleImageRecord, 0, len(rows))
	seen := map[string]string{}
	for _, row := range rows {
		if row.Configuration == nil || row.Configuration.Name == nil || row.Configuration.Descriptor == nil || row.Configuration.Descriptor.Digest == nil {
			return nil, false
		}
		ref, digest := *row.Configuration.Name, *row.Configuration.Descriptor.Digest
		if _, _, ok := appleRepository(ref); !ok || !validAppleImageDigest(digest) {
			return nil, false
		}
		if previous, ok := seen[ref]; ok && previous != digest {
			return nil, false
		}
		seen[ref] = digest
		images = append(images, appleImageRecord{Reference: ref, Digest: digest})
	}
	return images, true
}

func appleImagesInUse(rt string, run RunFunc) (map[string]bool, map[string]bool, bool) {
	res := run([]string{rt, "ls", "--all", "--format", "json"}, psTimeout)
	if !res.Ran || res.RC != 0 {
		return nil, nil, false
	}
	var rows []appleContainerJSON
	if !appleJSONArray(res.Stdout, &rows) || rows == nil {
		return nil, nil, false
	}
	digests, refs := map[string]bool{}, map[string]bool{}
	for _, row := range rows {
		if row.Configuration == nil || row.Configuration.Image == nil || row.Configuration.Image.Reference == nil || row.Configuration.Image.Descriptor == nil || row.Configuration.Image.Descriptor.Digest == nil {
			return nil, nil, false
		}
		ref, digest := *row.Configuration.Image.Reference, *row.Configuration.Image.Descriptor.Digest
		if ref == "" || !validAppleImageDigest(digest) {
			return nil, nil, false
		}
		refs[ref], digests[digest] = true, true
	}
	return digests, refs, true
}

func appleCandidateImages(images []appleImageRecord, protected map[string]struct{}) ([]appleImageRecord, map[string][]string, bool) {
	byDigest := map[string][]string{}
	protectedDigest := map[string]bool{}
	unique := map[string]appleImageRecord{}
	order := make([]string, 0, len(images))
	seenRef := map[string]bool{}
	for _, image := range images {
		repo, tag, ok := appleRepository(image.Reference)
		if !ok || !isAppleJailRepository(repo) {
			continue
		}
		if previous, exists := unique[image.Reference]; exists && previous.Digest != image.Digest {
			return nil, nil, false
		}
		unique[image.Reference] = image
		if !seenRef[image.Reference] {
			order = append(order, image.Reference)
			seenRef[image.Reference] = true
		}
		byDigest[image.Digest] = append(byDigest[image.Digest], tag)
		if _, keep := protected[tag]; keep {
			protectedDigest[image.Digest] = true
		}
	}
	candidates := make([]appleImageRecord, 0, len(unique))
	for _, ref := range order {
		image := unique[ref]
		if !protectedDigest[image.Digest] {
			candidates = append(candidates, image)
		}
	}
	return candidates, byDigest, true
}

// pruneAppleOldImages uses Apple's JSON inventory and reference-based delete
// command. Apple deletion has no runtime in-use veto, so each guarded apply
// re-reads both authorities immediately before deleting a stored reference.
func pruneAppleOldImages(rt string, protected map[string]struct{}, protectedKnown, apply bool, run RunFunc, guard Guard, recheck func(tags []string) bool) ([]string, ImageReapDecline) {
	images, known := readAppleImages(rt, run)
	if !known {
		return []string{}, DeclineImagesUnreadable
	}
	candidates, tagsByDigest, valid := appleCandidateImages(images, protected)
	if !valid {
		return []string{}, DeclineImagesUnreadable
	}
	if len(candidates) == 0 {
		return []string{}, ""
	}
	if !protectedKnown {
		return []string{}, DeclineNoCurrentPointers
	}
	inUse, inUseRefs, known := appleImagesInUse(rt, run)
	if !known {
		return []string{}, DeclineRuntimeUnreachable
	}
	if !apply {
		selected := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			if !inUse[candidate.Digest] && !inUseRefs[candidate.Reference] {
				selected = append(selected, candidate.Reference)
			}
		}
		return selected, ""
	}
	removed := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if inUse[candidate.Digest] || inUseRefs[candidate.Reference] {
			continue
		}
		candidate := candidate
		check := func() bool {
			if recheck != nil && !recheck(tagsByDigest[candidate.Digest]) {
				return false
			}
			freshImages, ok := readAppleImages(rt, run)
			if !ok {
				return false
			}
			freshCandidates, freshTags, ok := appleCandidateImages(freshImages, protected)
			if !ok {
				return false
			}
			found := false
			for _, fresh := range freshCandidates {
				if fresh.Reference == candidate.Reference && fresh.Digest == candidate.Digest {
					found = true
					break
				}
			}
			if !found {
				return false
			}
			freshInUse, freshRefs, ok := appleImagesInUse(rt, run)
			if !ok || freshInUse[candidate.Digest] || freshRefs[candidate.Reference] {
				return false
			}
			for _, tag := range freshTags[candidate.Digest] {
				if _, keep := protected[tag]; keep {
					return false
				}
			}
			if recheck != nil && !recheck(freshTags[candidate.Digest]) {
				return false
			}
			inspect := run(image.ImageInspectCmd(rt, candidate.Reference), inspectTimeout)
			if !inspect.Ran || inspect.RC != 0 {
				return false
			}
			var inspected []appleImageJSON
			if !appleJSONArray(inspect.Stdout, &inspected) || len(inspected) != 1 || inspected[0].Configuration == nil ||
				inspected[0].Configuration.Name == nil || inspected[0].Configuration.Descriptor == nil ||
				inspected[0].Configuration.Descriptor.Digest == nil {
				return false
			}
			return *inspected[0].Configuration.Name == candidate.Reference &&
				*inspected[0].Configuration.Descriptor.Digest == candidate.Digest
		}
		deleted := false
		del := func() {
			res := run([]string{rt, "image", "delete", candidate.Reference}, rmiTimeout)
			deleted = res.Ran && res.RC == 0
		}
		if guard == nil {
			if check() {
				del()
			}
		} else {
			guard.Do(check, del)
		}
		if deleted {
			removed = append(removed, candidate.Reference)
		}
	}
	return removed, ""
}
