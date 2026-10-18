package paths

import "path/filepath"

// PackTreeRoot returns AGENTS_DIR/<cname>/pack-trees, the directory holding one workspace's
// PER-LAUNCH PACK TREES (docs/reference/pack-system.md#oq-pk2). A pack tree is the staged copy
// of every pack one launch selected, laid out as <tree>/_official/<name> for the embedded packs
// and <tree>/<slug> for configured ones. A podman jail binds its launch's tree read-only at
// /ctx/packs, Apple Container copies it into the jail's home at the fresh launch, and the
// macos-user bootstrap copies it into the root-owned state dir.
//
// Each launch stages a NEW tree under this root and never edits one afterwards (the
// maintainer's OQ-PK2 ruling, option (c)): an attach never re-stages, so a running jail keeps
// the tree it booted with until it stops. A tree goes once the one container that could hold
// it is known gone (run.forgetGoneContainer), or with the whole AGENTS_DIR/<cname> through
// prune.PruneOrphanAgentStaging, the reaper the home skeleton relies on too.
//
// ONE spelling of the location, for the reason HomeSkeletonRoot has one: the stager that
// creates a tree, the argv that binds it, the attach that reads a running jail's, and the
// collector that removes it all have to agree.
func PackTreeRoot(cname string) string { return filepath.Join(AgentsDir(), cname, "pack-trees") }

// LivePackTreeRecord returns the file naming the pack tree the RUNNING container of cname
// booted from: one line, the tree's directory name under PackTreeRoot. A fresh container launch
// writes it, holding the workspace launch lock, before its container starts; an attach, which
// takes the same lock, reads it to find the tree it must read instead of re-staging. It goes with
// the tree once the container is known gone.
//
// A host-side record rather than a question to the runtime, because the two container backends
// deliver the tree differently: podman binds it, which `inspect` could report, while Apple
// Container COPIES it into the jail's home, and a copy the jail can write is exactly what host
// code must never read (run.acPackRootRel says why). A dot-name, so it can never collide with a
// tree, whose name starts with a timestamp.
func LivePackTreeRecord(cname string) string {
	return filepath.Join(PackTreeRoot(cname), ".live")
}

// LegacyPackStagingDir returns AGENTS_DIR/<cname>/packs, the ONE shared staging tree every
// launch of a workspace wrote before per-launch trees. A jail launched before that change still
// binds it at /ctx/packs, so nothing writes it any more: an attach to such a jail reads it, and
// the first fresh container launch that finds no container of the workspace's name retires it
// (run.retireLegacyPackStaging).
func LegacyPackStagingDir(cname string) string { return filepath.Join(AgentsDir(), cname, "packs") }

// treeCopiesLeaf is TreeCopyRoot's name under AGENTS_DIR/<cname>.
const treeCopiesLeaf = "tree-copies"

// TreeCopyRoot returns AGENTS_DIR/<cname>/tree-copies, where a macos-user launch keeps its patched
// extensions' copies: one per extension and store entry, made whole and then renamed into place, so
// the next launch handed the same build reuses it instead of copying the tree out of the store
// again (internal/cli's reusableTreeCopy). It is a sibling of PackTreeRoot rather than inside a
// launch's tree, because a tree is one launch's and is removed with it. It goes with the whole
// AGENTS_DIR/<cname> (prune.PruneOrphanAgentStaging); a copy no live launch holds is reaped by the
// next launch that settles a copy of the same extension.
func TreeCopyRoot(cname string) string { return filepath.Join(AgentsDir(), cname, treeCopiesLeaf) }

// TreeCopyRootFor is TreeCopyRoot for the workspace whose pack tree is packTree (a directory under
// PackTreeRoot), for a caller holding the tree and not the cname.
func TreeCopyRootFor(packTree string) string {
	return filepath.Join(filepath.Dir(filepath.Dir(packTree)), treeCopiesLeaf)
}
