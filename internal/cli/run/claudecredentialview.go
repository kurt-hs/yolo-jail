package run

// claudecredentialview.go is the launch's half of the Claude credential view
// (docs/design/claude-login-without-interception.md): whether this launch delivers the machine's
// Claude login as a per-workspace view instead of through the interception, and, when it does,
// registering the workspace's view with the host broker so the jail starts with one.
//
// THE ONE SECOND PATH THIS CONCERN IS ALLOWED, AND ONLY UNTIL THE MEASURES PASS (CL-D10). OQ-CL1
// ruled that the view replaces the /etc/hosts entry, the CA and the terminator everywhere,
// deleted rather than switched, after §7's measures on a real Claude and a day on a real
// rootless host. So the view is opt-in on every backend until they pass (claudeview.DefaultOn,
// CL-D11): podman keeps the interception, and Apple Container and macos-user keep Claude
// refreshing its own login, as they do today.
// Every line here, the switch and its readers are deleted with the interception (CL-D7).

import (
	"path/filepath"

	"github.com/mschulkind-oss/yolo-jail/internal/claudeview"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/macosuser"
	"github.com/mschulkind-oss/yolo-jail/internal/oauthbroker"
)

// claudeCredentialView reports whether this launch delivers the Claude login as a credential
// view: the claude-oauth-broker loophole is on (the claude pack is selected and nothing
// superseded it, brokerLoopholeActive), and claudeview.SwitchEnv resolves on for rt.
//
// ONE PREDICATE FOR EVERY READER — the argv (loopholesRuntimeArgs), the jail-daemon payload
// (jailDaemonsFor), the Apple Container allow list (startLoopholes), the jail's environment and
// the registration below — because a launch that dropped the terminator and did not register
// the view, or registered a view and still linked the shared file, would leave Claude with no
// working login at all.
func (o *Options) claudeCredentialView(rt string, cfg *jsonx.OrderedMap) bool {
	if o.Getenv == nil {
		return false
	}
	return brokerLoopholeActive(cfg) && claudeview.Selected(rt, o.Getenv)
}

// claudeCredentialViewEnvArgs is the `-e YOLO_CLAUDE_CREDENTIAL_VIEW=1` a view launch hands a
// container jail: the RESOLVED decision, so the in-jail entrypoint skips the shared_credentials
// link for the view (entrypoint.skipsForCredentialView) without re-deriving a runtime default it
// cannot see. Nothing when the view is off, so an interception launch's argv is unchanged.
func (o *Options) claudeCredentialViewEnvArgs(rt string, cfg *jsonx.OrderedMap) []string {
	if !o.claudeCredentialView(rt, cfg) {
		return nil
	}
	return []string{"-e", claudeview.SwitchEnv + "=" + claudeview.ResolvedValue(true)}
}

// registerView is the seam registerClaudeCredentialView calls; a test replaces it.
var registerView = func(loc claudeview.Location, rt, cname string) (oauthbroker.RegisterResult, error) {
	oauthbroker.ConfigureStore()
	return oauthbroker.RegisterView(loc, rt, cname)
}

// registerClaudeCredentialView registers this workspace's view with the host broker and writes
// it from the machine's login, when the launch delivers the view. It runs after the host
// services started, because the broker's start is what creates the state directory the
// registration lives in. That broker is this jail's own where HD-R1 is built
// (hostdaemonperjail.go), and it keeps every registered view current while the jail runs.
//
// Never fatal. A launch whose registration failed still starts, and says why its Claude may lack
// a login; on macos-user it preserves the view-off path, while distinguishing the self-mode
// session-TMPDIR workaround from account mode's measured Keychain write hang. Refusing a launch
// would be a bigger loss for a credential that may be recoverable from another supported path.
//
// OQ-CL2's "the next launch says so" is here: a workspace a jail's /logout signed out is signed
// back in to the machine's login by this registration, and the line names both halves.
func (o *Options) registerClaudeCredentialView(rt, cname string, cfg *jsonx.OrderedMap) {
	if !o.claudeCredentialView(rt, cfg) {
		return
	}
	out := o.pr(o.Stderr)
	subdir := claudeview.HostSubdir(rt)
	if rt == "macos-user" && o.MacosUserMode == macosuser.ModeSelf { // parity: HonoredBy — registration follows MMP-D5 HOME; account mode keeps its undotted location
		homes := macosuser.LaunchHomes(macosuser.ModeSelf, o.Workspace, o.macosUserHostHome())
		// MMP-D5 makes self HOME the workspace's home state; place the view at the `.claude`
		// directory under that HOME, where CL-D8 says Claude reads without env overrides.
		subdir = filepath.Base(filepath.Join(homes.Home, filepath.Dir(claudeview.ViewRel)))
	}
	loc := claudeview.Location{Workspace: o.Workspace, Subdir: subdir}
	res, err := registerView(loc, rt, cname)
	if err != nil {
		if rt == "macos-user" && o.MacosUserMode == macosuser.ModeSelf { // parity: Warned — native self enrollment depended on the separate session-TMPDIR workaround
			out.printf("[yellow]Warning: could not register this workspace's Claude credential view "+
				"(%s); native self-mode /login adopted the view with session-TMPDIR and Claude "+
				"store-path workarounds. This launcher now aligns the view path but does not yet "+
				"set session TMPDIR. Relaunch with YOLO_CLAUDE_CREDENTIAL_VIEW=0 to use the "+
				"existing view-off path.[/yellow]", err.Error())
		} else if rt == "macos-user" { // parity: Warned — account-mode Keychain writes hang; the launch recommends view-off rather than /login
			out.printf("[yellow]Warning: could not register this workspace's Claude credential view "+
				"(%s); account-mode /login has a measured Keychain write hang. Relaunch with "+
				"YOLO_CLAUDE_CREDENTIAL_VIEW=0 to use the existing view-off path.[/yellow]", err.Error())
		} else {
			out.printf("[yellow]Warning: could not register this workspace's Claude credential view "+
				"(%s); Claude in this jail will ask for /login.[/yellow]", err.Error())
		}
		return
	}
	if res.WasLoggedOut {
		out.print("[yellow]Claude: /logout in this workspace's jail had signed it out of the " +
			"machine's login; this launch signs it back in. `yolo claude-auth logout` signs the " +
			"whole machine out.[/yellow]")
	}
	if !res.Wrote {
		if rt == "macos-user" && o.MacosUserMode == macosuser.ModeSelf { // parity: Warned — self enrollment was measured only with the separate session-TMPDIR workaround
			out.print("[dim]Claude: this machine has no Claude login yet; native self-mode /login " +
				"adopted a view only with session-TMPDIR and Claude store-path workarounds. This " +
				"launcher now aligns the view path but does not yet set session TMPDIR. Relaunch " +
				"with YOLO_CLAUDE_CREDENTIAL_VIEW=0 to use the existing view-off path.[/dim]")
		} else if rt == "macos-user" { // parity: Warned — account-mode Keychain writes hang; the launch recommends view-off rather than /login
			out.print("[dim]Claude: this machine has no Claude login yet; account-mode /login has a " +
				"measured Keychain write hang. Relaunch with YOLO_CLAUDE_CREDENTIAL_VIEW=0 " +
				"to use the existing view-off path.[/dim]")
		} else {
			out.print("[dim]Claude: this machine has no Claude login yet; /login in the jail " +
				"enrolls it for every workspace.[/dim]")
		}
	}
}
