package entrypoint

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/claudesecurity"
	"github.com/mschulkind-oss/yolo-jail/internal/claudeview"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

const claudeSecurityAdapterDir = ".yolo/bin/block/claude-security"

// generateClaudeSecurityAdapter carries only an explicit, resolved account-view decision from the
// native launch. It is not a host environment switch and is absent for self, view-off and other
// backends.
func generateClaudeSecurityAdapter(e *Env) error {
	if e.Getenv(claudesecurity.EnvVar) != claudesecurity.AccountMode {
		return nil
	}
	packs, err := LoadJailPacks(e)
	if err != nil {
		return err
	}
	agent := claudeCredentialViewAgent(e, packs)
	if agent == "" {
		return nil
	}
	path := filepath.Join(e.Home, filepath.FromSlash(claudeSecurityAdapterDir), agent, "security")
	if _, err := os.Lstat(filepath.Join(e.BlockDir(), "security")); err == nil {
		e.warn("not installing Claude's account-mode security adapter: an existing security block or intercept takes precedence")
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return writeExecutable(path, claudesecurity.ShimContent("yolo", claudesecurity.AccountMode, ""))
}

// claudeCredentialViewAgent recognizes the program whose selected pack declares the existing
// Claude credential-view hook. The hook's file identifies the home subtree; the same pack must
// declare the program that owns it, so other selected programs never acquire this adapter.
func claudeCredentialViewAgent(e *Env, packs []*packload.Pack) string {
	for _, p := range packs {
		if p == nil || p.Decl == nil {
			continue
		}
		for _, h := range p.Decl.HookContributions() {
			if h.Name != HookSharedCredentials || path.Clean(filepath.ToSlash(h.File)) != claudeview.ViewRel ||
				h.SharedDir == "" || !declaresSharedDir(p, h.SharedDir) || !e.skipsForCredentialView(h) {
				continue
			}
			agent := strings.TrimPrefix(path.Base(path.Dir(claudeview.ViewRel)), ".")
			for _, in := range p.Decl.InstallContributions() {
				if in.Bin == agent {
					return agent
				}
			}
		}
	}
	return ""
}
