package image

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// packagesLaunchKey covers config-declared packages, ambient package bytes, and image
// attributes independently. Moving nixpkgs references never acquire a record.
func (o *AutoLoadOptions) packagesLaunchKey(identity string) string {
	arch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[runtime.GOARCH]
	if arch == "" {
		return ""
	}
	return o.packagesLaunchKeyForSystem(identity, arch+"-"+runtime.GOOS)
}

func (o *AutoLoadOptions) packagesLaunchKeyForSystem(identity, system string) string {
	if o.SkipBuild || (len(o.ExtraPackages) == 0 && o.AmbientExtraPackages == "" && (o.Attr == "" || o.Attr == ImageAttrDefault)) {
		return ""
	}
	id, ok := ParseImageIdentity(identity)
	if !ok {
		return ""
	}
	if !fixedPackagePins(o.ExtraPackages) {
		return ""
	}
	if o.AmbientExtraPackages != "" {
		var ambient []any
		if json.Unmarshal([]byte(o.AmbientExtraPackages), &ambient) != nil || !fixedPackagePins(ambient) {
			return ""
		}
	}
	packages := ""
	if len(o.ExtraPackages) != 0 {
		var err error
		packages, err = jsonx.DumpsCompact(o.ExtraPackages)
		if err != nil {
			return ""
		}
	}
	attr := o.Attr
	if attr == "" {
		attr = ImageAttrDefault
	}
	if attr != ImageAttrDefault && attr != ImageAttrLean {
		return ""
	}
	if system != "x86_64-linux" && system != "aarch64-linux" && system != "x86_64-darwin" && system != "aarch64-darwin" {
		return ""
	}
	input, _ := json.Marshal([]string{id, packages, o.AmbientExtraPackages, attr, system})
	digest := sha256.Sum256(input)
	return hex.EncodeToString(digest[:])
}

func fixedPackagePins(packages []any) bool {
	for _, p := range packages {
		if spec, ok := p.(map[string]any); ok {
			if pin, exists := spec["nixpkgs"]; exists {
				s, ok := pin.(string)
				if !ok || len(s) != 40 || strings.TrimLeft(s, "0123456789abcdefABCDEF") != "" {
					return false
				}
			}
		}
	}
	return true
}

func launchRecordPath(key string) string {
	return filepath.Join(paths.BuildDir(), "launch-images", key)
}

// The content ref is derived from the recorded store path, not supplied by the
// record. Both runtime presence and nix validity must be proven before reuse.
func (o *AutoLoadOptions) recordedPackagesImage(key string) (string, string) {
	if key == "" {
		return "", ""
	}
	data, err := os.ReadFile(launchRecordPath(key))
	if err != nil {
		return "", ""
	}
	p := strings.TrimSpace(string(data))
	if !filepath.IsAbs(p) || !o.StorePathValid(p) {
		return "", ""
	}
	ref := JailImageRef(o.Runtime, p)
	if !ConfirmLoaded(o.Runtime, ref, p, o.LockHousekeeping, o.Run) {
		return "", ""
	}
	return ref, p
}

func (o *AutoLoadOptions) recordPackagesImage(key, storePath string) {
	if key == "" {
		return
	}
	path := launchRecordPath(key)
	err := os.MkdirAll(filepath.Dir(path), 0755)
	if err == nil {
		var f *os.File
		f, err = os.CreateTemp(filepath.Dir(path), ".record-*")
		if err == nil {
			defer os.Remove(f.Name())
			_, err = f.WriteString(storePath + "\n")
			closeErr := f.Close()
			if err == nil {
				err = closeErr
			}
			if err == nil {
				err = os.Rename(f.Name(), path)
			}
		}
	}
	if err != nil {
		fmt.Fprintln(o.reportWriter(o.Out), "Warning: could not record this launch's image inputs — the next launch will rebuild; check permissions on "+filepath.Dir(path)+": "+err.Error())
	}
}
