package cli

import (
	"context"
	"testing"
	"time"
)

func fakeAppleListing(t *testing.T, out string, err error) *int {
	t.Helper()
	old := applePreparationListing
	calls := new(int)
	applePreparationListing = func(ctx context.Context) (string, error) {
		*calls++
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 2*time.Second {
			t.Error("unbounded runtime query")
		}
		return out, err
	}
	t.Cleanup(func() { applePreparationListing = old })
	return calls
}
