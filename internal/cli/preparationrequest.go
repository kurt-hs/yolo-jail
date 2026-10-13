package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
)

type freshRequestInputs struct {
	Owner    string              `json:"owner"`
	Source   string              `json:"source"`
	Platform string              `json:"platform"`
	Inputs   packsrc.CheckInputs `json:"inputs"`
	Recipe   string              `json:"recipe"`
}

func registerFreshPreparation(ctx context.Context, store *packsrc.Store, f packload.Fork, platform string,
	identities map[string]string, boundaries map[string]uint64, errs map[string]error) error {
	key := f.Key()
	identity, err := freshPreparationIdentity(f, platform)
	if err == nil {
		var seq uint64
		seq, err = store.FreshCheckBoundaryContext(ctx, key)
		if err == nil {
			identities[key], boundaries[key] = identity, seq
			return nil
		}
	}
	errs[key] = err
	return err
}

// freshPreparationIdentity freezes the authorized source/check vector and build recipe at request
// registration. Advances compare it again before checking, so an edit never retargets a request.
func freshPreparationIdentity(f packload.Fork, platform string) (string, error) {
	series, err := f.ReadSeries()
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
	data, err := json.Marshal(freshRequestInputs{Owner: f.Key(), Source: f.Source, Platform: platform, Inputs: inputs, Recipe: recipe})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
