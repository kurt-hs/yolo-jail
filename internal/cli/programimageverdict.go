package cli

// programimageverdict.go keeps the sealed producer's answer that the jail image already provides a
// declared program (npmImageProvidedResolution), so the answer costs a producer jail at most once
// per image.
//
// THE KEY IS THE IMAGE IDENTITY PLUS THE PROGRAM NAME, and nothing else. Whether `/bin/<name>`
// exists is a fact about the image's contents, which its identity (`/etc/yolo-jail-image-identity`,
// the same value `nix eval .#imageIdentity` gives the launch) names exactly; the pack, the npm
// package and its selector cannot change it. A new image identity is a different key, so it asks
// again. The verdict is written under the identity the PRODUCER read inside the image and looked
// up under the identity the LAUNCH evaluated: when the two disagree the lookup misses and the
// launch asks, which is the safe direction.
//
// It lives beside the ordinary resolution records (program-resolutions/ in the capture store),
// the other per-program preparation verdicts.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	"github.com/mschulkind-oss/yolo-jail/internal/image"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
)

// imageProvidesVerdict is one kept answer: the image of ImageIdentity provides Bin at Path.
type imageProvidesVerdict struct {
	ImageIdentity string
	Bin           string
	Path          string
}

func imageProvidesVerdictPath(store *capture.Store, identity, bin string) string {
	sum := sha256.Sum256([]byte(identity + "\x00" + bin))
	return filepath.Join(store.Dir, "program-resolutions", "image-provides", hex.EncodeToString(sum[:])+".json")
}

// recordImageProvides keeps the producer's verdict. It refuses an identity that is not one
// (image.ParseImageIdentity), so an image that cannot name itself is asked every time rather than
// cached under a name every such image would share.
func recordImageProvides(store *capture.Store, identity, bin, path string) error {
	id, ok := image.ParseImageIdentity(identity)
	if !ok {
		return fmt.Errorf("the producer's image names no valid identity (%q)", identity)
	}
	if !packdecl.ValidBinName(bin) || !filepath.IsAbs(path) {
		return fmt.Errorf("invalid image verdict for %q at %q", bin, path)
	}
	file := imageProvidesVerdictPath(store, id, bin)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(imageProvidesVerdict{ImageIdentity: id, Bin: bin, Path: path})
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(file), ".verdict-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), file)
}

// imageProvidesVerdictFor returns the kept path for bin in the image of identity, if any.
func imageProvidesVerdictFor(store *capture.Store, identity, bin string) (string, bool) {
	id, ok := image.ParseImageIdentity(identity)
	if !ok {
		return "", false
	}
	raw, err := os.ReadFile(imageProvidesVerdictPath(store, id, bin))
	if err != nil {
		return "", false
	}
	var v imageProvidesVerdict
	if json.Unmarshal(raw, &v) != nil || v.ImageIdentity != id || v.Bin != bin || !filepath.IsAbs(v.Path) {
		return "", false
	}
	return v.Path, true
}

// skipImageProvidedPrograms drops from vector every program a kept verdict says this launch's
// image already provides, saying so for each. It runs before the timer registration and before any
// producer, so a dropped program costs neither. identity is the launch's image identity, asked only
// when there is something to drop; nil, or an identity the launch could not evaluate, keeps the
// whole vector (every program then asks its producer, as before the verdict existed).
func skipImageProvidedPrograms(vector []npmCaptureInput, identity func() (string, bool), store *capture.Store, w io.Writer) []npmCaptureInput {
	if len(vector) == 0 || identity == nil {
		return vector
	}
	id, ok := identity()
	if !ok {
		return vector
	}
	kept := vector[:0:0]
	for _, in := range vector {
		if path, hit := imageProvidesVerdictFor(store, id, in.Bin); hit {
			fmt.Fprintf(w, "%s: the jail image already provides %s, so nothing is prepared and the image's copy runs — drop the pack's `program %s` declaration to skip this check\n", in.Bin, path, in.Bin)
			continue
		}
		kept = append(kept, in)
	}
	return kept
}
