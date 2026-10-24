package ghbroker

import (
	"context"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/brokerscope"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// A READ GRANT WITHDRAWN DURING THE APPROVAL WAIT REFUSES THE WRITE (docs/design/boundary-broker.md
// BB-D108). A write files its request while the workspace holds a read grant; a host `yolo config
// apply` then rewrites the live scope file without one. The approval gate revalidates the request
// through Authority.Revalidate, which classifies against that freshly read file, so the approved
// write is refused rather than run.
func TestAReadGrantWithdrawnDuringTheWaitRefusesTheWrite(t *testing.T) {
	b, ran := windowFixture(t)
	b.trackJail(paths.JailShortHash(b.authority.scope.Container))
	if got := callWindow(b, context.Background()); got != ExitTempFail {
		t.Fatalf("first call %d, want pending", got)
	}
	rs, _, err := b.store.List()
	if err != nil || len(rs) != 1 {
		t.Fatalf("pending requests %v (%v), want one", rs, err)
	}
	if _, err := b.authority.Revalidate(rs[0]); err != nil {
		t.Fatalf("with the read grant held, the request did not revalidate: %v", err)
	}
	f, err := b.authority.live()
	if err != nil {
		t.Fatal(err)
	}
	if err := brokerscope.ReplaceScopeContext(context.Background(), b.authority.path, f, f.Repos, nil); err != nil {
		t.Fatal(err)
	}
	_, err = b.authority.Revalidate(rs[0])
	if err == nil || !strings.Contains(err.Error(), "holds no read grant") {
		t.Fatalf("after the read grant was withdrawn the request revalidated (err %v), want the no-read refusal", err)
	}
	if ran.Load() != 0 {
		t.Fatalf("gh ran %d times", ran.Load())
	}
}
