package hostfloor

import (
	"context"
	"testing"
)

func TestEnsureFreshBypassesPolicyAndRecentStampWithoutReinstall(t *testing.T) {
	w := newWorld(t)
	w.publish("opencode-ai", "1.2.3", "bin=opencode")
	p := npmProgram("opencode", "opencode", "opencode-ai")
	first, _, err := w.floor.Ensure(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	w.floor.UpdatesAllowed = func(string) bool { return false }
	st, outcome, err := w.floor.EnsureFresh(context.Background(), p)
	if err != nil || outcome != Current || st.Record.Dir != first.Record.Dir {
		t.Fatalf("unchanged fresh check reinstalled: %s %v", outcome, err)
	}
	w.publish("opencode-ai", "1.3.0", "bin=opencode")
	st, outcome, err = w.floor.EnsureFresh(context.Background(), p)
	if err != nil || outcome != Updated || st.Record.Version != "1.3.0" {
		t.Fatalf("manual authorization lost: %s %+v %v", outcome, st.Record, err)
	}
}
