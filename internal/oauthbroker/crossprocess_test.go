package oauthbroker

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/oauthterminator"
)

// crossprocess_test.go pins that the broker's memory of the refresh tokens it replaced or
// refused is SHARED by every broker process on the machine, not kept by each for itself.
//
// Each jail runs its own claude-oauth-broker (docs/design/host-daemon-ownership.md, HD-R1), and
// every one of them refreshes the one machine login under the one flock. So the refresh race the
// caller check tolerates — a Claude read the shared file just before ANOTHER broker rotated it —
// is now a race between processes, and a token jail B's broker replaced must be one jail A's
// broker still recognizes. Each test stands in for a second broker process by discarding this
// process's in-memory state between the two halves (asAnotherBrokerProcess); everything a real
// second process would still see — the state directory — is left in place.

// asAnotherBrokerProcess discards what one broker process holds in memory, standing in for the
// next request arriving at a different jail's broker.
func asAnotherBrokerProcess(t *testing.T) {
	t.Helper()
	recentlyReplaced = newReplacedTokens(replacedTokensFile)
	refusedEnrollments = newReplacedTokens(refusedEnrollmentsFile)
}

func TestAJailsBrokerServesAClaudeThatReadTheTokenAnotherJailsBrokerReplaced(t *testing.T) {
	var upstream atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstream.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"AT_new","refresh_token":"RT_new","expires_in":3600}`))
	}))
	defer srv.Close()
	t.Setenv("YOLO_BROKER_UPSTREAM_URL", srv.URL)
	dir := t.TempDir()
	RefreshLockPath = filepath.Join(dir, "refresh.lock")
	saved, savedRefused := recentlyReplaced, refusedEnrollments
	t.Cleanup(func() { RefreshLockPath, recentlyReplaced, refusedEnrollments = "", saved, savedRefused })
	asAnotherBrokerProcess(t)
	creds := seedCreds(t, dir, "RT_current")

	// Jail B's broker (its background tick, or its Claude's refresh) rotates the login.
	if res := DoRefresh(creds); func() bool { _, bad := res.Get("error"); return bad }() {
		t.Fatalf("jail B's refresh failed: %v", res)
	}
	if upstream.Load() != 1 {
		t.Fatalf("upstream refreshes = %d, want 1", upstream.Load())
	}

	// Jail A's Claude read RT_current a moment before, and POSTs to jail A's broker.
	asAnotherBrokerProcess(t)
	endpoint := serveBroker(t, creds)
	res := oauthterminator.Refresh(endpoint, "RT_current")
	if res.Status != http.StatusOK || !strings.Contains(string(res.Body), "AT_new") {
		t.Fatalf("jail A's Claude, holding the token jail B's broker just replaced, got %d %s; "+
			"want 200 from the cache — a 401 here is a jail's Claude failing a refresh because "+
			"another jail's broker won the race", res.Status, res.Body)
	}
	if upstream.Load() != 1 {
		t.Errorf("upstream refreshes = %d, want the cache to answer jail A", upstream.Load())
	}
	// And a stranger is still refused.
	if res := oauthterminator.Refresh(endpoint, "RT_stranger"); res.Status != http.StatusUnauthorized {
		t.Errorf("a stranger got %d %s, want 401", res.Status, res.Body)
	}
}

// A view a jail's Claude wrote back holding a token another broker already replaced is only
// stripped: redeeming it would spend an upstream request on a dead token at best, and at worst
// adopt it as the machine's login.
func TestAViewHoldingATokenAnotherBrokerReplacedIsStrippedNotRedeemed(t *testing.T) {
	f := newViewFixture(t)
	writeLogin(t, CanonicalPath, "AT_old", "RT_old", nowMS()+20*60_000, nil)
	alpha := f.workspace(t, "alpha")

	// Broker one refreshes the machine login ahead of expiry: RT_old is replaced by RT_1.
	BackgroundRefreshTick(f.legacy, BackgroundRefreshLeadSeconds)
	if got := f.redemptions(); len(got) != 1 || got[0] != "RT_old" {
		t.Fatalf("upstream was sent %v, want the canonical RT_old once", got)
	}
	// Alpha's Claude still held RT_old in memory and saved it back into its view (CL-D13).
	writeLogin(t, alpha.Path(), "AT_old", "RT_old", nowMS()+20*60_000, nil)

	asAnotherBrokerProcess(t)
	BackgroundRefreshTick(f.legacy, BackgroundRefreshLeadSeconds)
	if got := f.redemptions(); len(got) != 1 {
		t.Fatalf("upstream was sent %v: another jail's broker redeemed a token the first "+
			"broker had already replaced", got)
	}
	if got := str(oauthOf(t, CanonicalPath), "refreshToken"); got != "RT_1" {
		t.Errorf("the canonical refresh token = %q, want RT_1 kept", got)
	}
	assertView(t, alpha, "AT_1")
}

func TestAnEnrollmentOneBrokerRefusedIsNotRetriedByAnother(t *testing.T) {
	f := newViewFixture(t)
	writeLogin(t, CanonicalPath, "AT_machine", "RT_machine", nowMS()+7*3600_000, nil)
	alpha := f.workspace(t, "alpha")
	writeLogin(t, alpha.Path(), "AT_jail", "RT_dead", nowMS()+8*3600_000, nil)
	f.status = http.StatusBadRequest

	BackgroundRefreshTick(f.legacy, BackgroundRefreshLeadSeconds)
	writeLogin(t, alpha.Path(), "AT_jail", "RT_dead", nowMS()+8*3600_000, nil)
	asAnotherBrokerProcess(t)
	BackgroundRefreshTick(f.legacy, BackgroundRefreshLeadSeconds)
	if got := f.redemptions(); len(got) != 1 {
		t.Fatalf("upstream was sent %v, want one attempt machine-wide, not one per broker", got)
	}
	assertView(t, alpha, "AT_machine")
}

// The shared memory is bounded like the in-process one was, and is kept beside the refresh lock.
func TestTheSharedReplacedTokenMemoryIsBoundedAndBesideTheLock(t *testing.T) {
	dir := t.TempDir()
	RefreshLockPath = filepath.Join(dir, "refresh.lock")
	t.Cleanup(func() { RefreshLockPath = "" })
	r := newReplacedTokens(replacedTokensFile)
	for i := 0; i <= replacedTokenMemory; i++ {
		r.record("RT_" + string(rune('a'+i)))
	}
	other := newReplacedTokens(replacedTokensFile)
	if other.has("RT_a") {
		t.Error("the oldest replaced token is still presentable past the memory bound")
	}
	if !other.has("RT_" + string(rune('a'+replacedTokenMemory))) {
		t.Error("another process does not see the newest replaced token")
	}
	if got := r.path(); got != filepath.Join(dir, replacedTokensFile) {
		t.Errorf("the shared memory is at %s, want beside the refresh lock", got)
	}
}
