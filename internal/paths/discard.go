package paths

import "path/filepath"

// DiscardRoot returns AGENTS_DIR/<cname>/.discard, where a jail's teardown moves the per-launch
// directories it is done with (the home skeleton, the pack tree and the tree's per-launch copies of
// patched extensions) once their container is known gone, for a detached remover to delete
// (run.discardGoneTrees). A move within one AGENTS_DIR/<cname> is one rename however large the
// tree, so the teardown the user's prompt waits on no longer pays for the deletion.
//
// A dot-name, so it collides with no tree, skeleton or skills staging directory, and nothing binds
// it into a jail. Whatever a remover never reached goes with the whole AGENTS_DIR/<cname> through
// prune.PruneOrphanAgentStaging, or with the next teardown's remover, which empties the whole root.
func DiscardRoot(cname string) string { return filepath.Join(AgentsDir(), cname, ".discard") }
