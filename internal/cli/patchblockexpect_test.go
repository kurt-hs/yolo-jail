package cli

// short8 is a commit as the patch failure block's ERROR line names it (packsrc.PatchFailure.Render):
// its first eight characters.
func short8(commit string) string {
	if len(commit) > 8 {
		return commit[:8]
	}
	return commit
}
