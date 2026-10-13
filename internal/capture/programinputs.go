package capture

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// ProgramInputs describes a resolved ordinary install, not a workspace or a launch.
// Release and dependency identities must come from resolution, never from a guessed
// latest version. Recipe covers the selected install and declared refresh instructions.
// Runtime identifies the producer's toolchain/image, not merely its backend name.
type ProgramInputs struct {
	Owner, Bin, Declared, Resolver string
	Release, Source, Recipe        string
	Dependencies                   []string
	Runtime, Platform, NodeABI     string
}

// Identity is the exact compatibility key carried by an ordinary capture receipt.
// Old receipts lacking this key keep their legacy selection; they cannot satisfy it.
func (p ProgramInputs) Identity() (string, error) {
	for _, f := range []struct{ name, value string }{{"owner", p.Owner}, {"bin", p.Bin}, {"declaration", p.Declared}, {"resolver", p.Resolver}, {"release", p.Release}, {"recipe", p.Recipe}, {"runtime", p.Runtime}, {"platform", p.Platform}} {
		if strings.TrimSpace(f.value) == "" {
			return "", fmt.Errorf("program capture: missing resolved %s — resolve the selected install before preparing it", f.name)
		}
	}
	if p.Resolver == "npm" && p.NodeABI == "" {
		return "", fmt.Errorf("program capture: missing observed Node ABI — resolve the producer's Node runtime before preparing %s", p.Bin)
	}
	p.Dependencies = slices.Clone(p.Dependencies)
	slices.Sort(p.Dependencies)
	p.Dependencies = slices.Compact(p.Dependencies)
	if p.Dependencies == nil {
		p.Dependencies = []string{}
	}
	b, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(b)
	return hex.EncodeToString(digest[:]), nil
}
