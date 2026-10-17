package oauthbroker

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"
)

// TestTheBrokerLogsAnEmptyOrBrokenSharedFileOncePerChange drives the daemon's own tick
// (BackgroundRefreshTick, which syncs the canonical login from the shared file every minute)
// over a shared credentials file that is empty, then broken, and asserts each state reaches the
// log once, however many ticks see it: a file left empty for months used to add a line a minute.
func TestTheBrokerLogsAnEmptyOrBrokenSharedFileOncePerChange(t *testing.T) {
	f := newViewFixture(t)
	var buf bytes.Buffer
	setupLogWriter(&buf, false)
	t.Cleanup(func() { logger = nil; logVerbose = false })
	resetSharedFileState()
	t.Cleanup(resetSharedFileState)

	tick := func(n int) {
		for range n {
			BackgroundRefreshTick(f.legacy, BackgroundRefreshLeadSeconds)
		}
	}
	count := func(sub string) int { return strings.Count(buf.String(), sub) }

	// The placeholder every machine has until a jail's Claude signs in.
	if err := os.WriteFile(f.legacy, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	tick(3)
	if got := count("the shared credentials file is empty"); got != 1 {
		t.Fatalf("an empty shared file logged %d times over three ticks, want 1:\n%s", got, buf.String())
	}
	if got := count("does not parse"); got != 0 {
		t.Fatalf("an empty shared file was logged as a parse failure:\n%s", buf.String())
	}

	// Something wrote half a file: a new state, logged once.
	if err := os.WriteFile(f.legacy, []byte(`{"claudeAiOauth": {`), 0o600); err != nil {
		t.Fatal(err)
	}
	tick(3)
	if got := count("the shared credentials file does not parse"); got != 1 {
		t.Fatalf("a broken shared file logged %d times over three ticks, want 1:\n%s", got, buf.String())
	}

	// The same broken bytes rewritten later are a change of the file, and logged again.
	later := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(f.legacy, later, later); err != nil {
		t.Fatal(err)
	}
	tick(2)
	if got := count("the shared credentials file does not parse"); got != 2 {
		t.Fatalf("a rewritten broken shared file logged %d times in all, want 2:\n%s", got, buf.String())
	}

	// A login lands, then the file is emptied again: the empty state is news once more.
	writeLogin(t, f.legacy, "AT_1", "RT_1", time.Now().Add(8*time.Hour).UnixMilli(), nil)
	tick(1)
	if err := os.WriteFile(f.legacy, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	tick(3)
	if got := count("the shared credentials file is empty"); got != 2 {
		t.Fatalf("an emptied shared file logged %d times in all, want 2:\n%s", got, buf.String())
	}
	// AN EMPTIED FILE IS NOT A SIGN-OUT: the canonical login it was adopted into is still there.
	canonical, err := oauthFromCreds(CanonicalPath)
	if err != nil {
		t.Fatalf("the canonical login is gone after the shared file was emptied: %v", err)
	}
	if at, _ := stringField(canonical, "accessToken"); at != "AT_1" {
		t.Errorf("the canonical login changed after the shared file was emptied: access token %q", at)
	}
}
