package cli

// patchedreplayempty_test.go pins the REPORTED reason for an ORIGINALLY EMPTY member of a patch
// series (internal/packsrc/replay.go, `pick`; docs/design/patched-forks.md §5, PF-D6, PF-D29): a
// member whose commit changed nothing when it was written — its tree is its parent's — makes no
// change of its own, and the replay's pick changes nothing either, but NOTHING about it is upstream.
// It must not be reported as "already upstream" (the reason a member the upstream already holds
// gets); it is reported as empty when written, beside the absorbed member that keeps the upstream
// line. Every surface renders the one pick, so this pins it where the user sees it: `yolo pack
// series check`, `yolo pack update` and `yolo pack rebase`, three verbs over the shared result.

import (
	"path/filepath"
	"strings"
	"testing"
)

// noopMemberPatch is a mail-format patch `git am` applies to make an EMPTY commit: two hunks that
// change line 20 and change it back, so the committed tree is its parent's. It has a `diff --git`,
// so the series reader takes it (a member with no diff at all is refused).
const noopMemberPatch = `From 2222222222222222222222222222222222222222 Mon Sep 17 00:00:00 2001
From: t <t@e>
Date: Mon, 1 Jan 2024 00:00:00 +0000
Subject: [PATCH 2/2] noop

---
 f.txt | 2 +-
 1 file changed, 1 insertion(+), 1 deletion(-)

diff --git a/f.txt b/f.txt
index 9bf6424..3333333 100644
--- a/f.txt
+++ b/f.txt
@@ -17,7 +17,7 @@
 17
 18
 19
-20
+twenty
 21
 22
 23
diff --git a/f.txt b/f.txt
index 3333333..9bf6424 100644
--- a/f.txt
+++ b/f.txt
@@ -17,7 +17,7 @@
 17
 18
 19
-twenty
+20
 21
 22
 23
-- 
2.55.0
`

// writeEmptySecondMember rewrites the fixture's second member with the no-op patch, so the series
// is: member one changes line 10, member two changes nothing.
func writeEmptySecondMember(t *testing.T, f *patchedFixture) {
	t.Helper()
	writeFile(t, filepath.Join(f.forkDir, "patches", "0002-twelve.patch"), noopMemberPatch)
}

// AN ORIGINALLY EMPTY MEMBER IS NOT ALREADY UPSTREAM: with v1.1.0 holding member one's change and
// member two empty when written, every rendering verb says member one is already in upstream v1.1.0
// and member two was empty when written — never that member two is already upstream.
func TestAnOriginallyEmptyMemberIsNotReportedAsAlreadyUpstream(t *testing.T) {
	f := newPatchedFixture(t, "")
	writeEmptySecondMember(t, f)
	f.commit(t, "v1.1.0", map[int]string{10: "ten"})

	const (
		emptyLine    = "0002-twelve.patch was empty when written and makes no change of its own; drop it from patches"
		upstreamLine = "0001-ten.patch is already in upstream v1.1.0 ("
		mislabeled   = "0002-twelve.patch is already in upstream"
	)
	for _, verb := range []struct {
		name string
		run  func() (int, string, string)
	}{
		{"series check", func() (int, string, string) { return seriesVerb(t, "check", f.forkDir) }},
		{"update", func() (int, string, string) { return packVerb(t, "update") }},
		{"rebase", func() (int, string, string) {
			return rebaseVerb(t, "forkpack/tool", "--into", filepath.Join(t.TempDir(), "clone"))
		}},
	} {
		rc, out, errw := verb.run()
		if rc != 0 {
			t.Errorf("%s rc=%d\n%s\n%s", verb.name, rc, out, errw)
			continue
		}
		if !strings.Contains(out, emptyLine) || !strings.Contains(out, upstreamLine) || strings.Contains(out, mislabeled) {
			t.Errorf("%s: want the empty member reported empty (%q) and the absorbed member upstream (%q), "+
				"not %q\n%s", verb.name, emptyLine, upstreamLine, mislabeled, out)
		}
	}
}
