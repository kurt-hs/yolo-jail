package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"os"

	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
)

type freshRequestInputs struct {
	Owner     string              `json:"owner"`
	Base      string              `json:"base,omitempty"`
	TreeOwner string              `json:"tree_owner,omitempty"`
	Into      string              `json:"into,omitempty"`
	Source    string              `json:"source"`
	Platform  string              `json:"platform"`
	Platforms []string            `json:"platforms,omitempty"`
	Inputs    packsrc.CheckInputs `json:"inputs"`
	Recipe    string              `json:"recipe"`
	Config    []byte              `json:"config,omitempty"`
}

func registerFreshPreparation(ctx context.Context, store *packsrc.Store, f packload.Fork, platform string,
	identities map[string]string, boundaries map[string]packsrc.CheckStart, errs map[string]error) error {
	key := f.Key()
	identity, err := freshPreparationIdentity(f, platform)
	if err == nil {
		var boundary packsrc.CheckStart
		local := *store
		local.Ctx = ctx
		boundary, err = local.CheckBoundary(key)
		if err == nil {
			identities[key], boundaries[key] = identity, boundary
			return nil
		}
	}
	errs[key] = err
	return err
}

// freshPreparationIdentity freezes the authorized source/check vector and build recipe at request
// registration. Advances compare it again before checking, so an edit never retargets a request.
func freshPreparationIdentity(f packload.Fork, platform string) (string, error) {
	var series *packsrc.Series
	var err error
	if f.Patched() || f.IsTree() {
		series, err = f.DeclaredSeries() // a patch branch: its address, never the commit a check records
	}
	if err != nil {
		return "", err
	}
	inputs, _, _, err := f.CheckWant(series).Inputs()
	if err != nil {
		return "", err
	}
	recipe := (forkBuild{Fork: f, Series: series}).recipe()
	if recipe == "" {
		return "", fmt.Errorf("%s has no supported build recipe", f.Key())
	}
	configBytes, err := os.ReadFile(paths.UserConfigPath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	data, err := json.Marshal(freshRequestInputs{Config: configBytes, Owner: f.Key(), Base: f.Base, TreeOwner: f.Owner, Into: f.Into, Source: f.Source, Platform: platform, Platforms: f.Platforms, Inputs: inputs, Recipe: recipe})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
