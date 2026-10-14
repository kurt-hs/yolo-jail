package run

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
)

func TestOrdinaryProgramCaptureCallerClassifiesDecodedCAAndProxyInputs(t *testing.T) {
	for _, tc := range []struct {
		name, config, dotenv, wantKey, wantReason string
		wantUnsupported                           bool
	}{
		{name: "no-profile-baseline", config: `{}`},
		{name: "inline-lowercase-proxy", config: `{"env_sources":[{"https_proxy":"http://proxy.invalid:3128"}]}`, wantKey: "https_proxy", wantUnsupported: true},
		{name: "inline-mixed-case-ca", config: `{"env_sources":[{"sSl_CeRt_FiLe":"/fixture/custom-ca.pem"}]}`, wantKey: "sSl_CeRt_FiLe", wantUnsupported: true},
		{name: "dotenv-proxy", config: `{"env_sources":[".capture.env"]}`, dotenv: "hTtPs_PrOxY=http://proxy.invalid:3128\n", wantKey: "hTtPs_PrOxY", wantUnsupported: true},
		{name: "dotenv-source-classification", config: `{"env_sources":[".capture.env"]}`, dotenv: "ORDINARY_SETTING=value\n", wantReason: "file-backed env_sources", wantUnsupported: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var called int
			var request ProgramCaptureRequest
			argv, printed := fakePodmanLaunch(t, func(o *Options) {
				o.AcceptConfigChanges = true
				repo, ok := o.RepoRoot()
				if !ok {
					t.Fatal("fake launch has no repository root")
				}
				for _, name := range []string{"flake.nix", "flake.lock"} {
					if err := os.WriteFile(filepath.Join(repo.Root, name), []byte("{}"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.WriteFile(filepath.Join(o.Workspace, config.WorkspaceConfigName), []byte(tc.config), 0o600); err != nil {
					t.Fatal(err)
				}
				if tc.dotenv != "" {
					if err := os.WriteFile(filepath.Join(o.Workspace, ".capture.env"), []byte(tc.dotenv), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				o.PreparePrograms = func(req ProgramCaptureRequest) (map[string]entrypoint.ProgramCaptureDelivery, error) {
					called++
					request = req
					return map[string]entrypoint.ProgramCaptureDelivery{}, nil
				}
			})
			if called != 1 || argv == nil {
				t.Fatalf("actual launch preparation caller not reached: calls=%d argv=%v\n%s", called, argv, printed)
			}
			if got := request.UnsupportedRuntime != ""; got != tc.wantUnsupported {
				t.Fatalf("decoded runtime unsupported=%t, want %t (reason %q)", got, tc.wantUnsupported, request.UnsupportedRuntime)
			}
			if tc.wantUnsupported && tc.wantKey != "" && !strings.Contains(strings.ToUpper(request.UnsupportedRuntime), strings.ToUpper(tc.wantKey)) {
				t.Fatalf("classifier did not name effective environment key %q: %q", tc.wantKey, request.UnsupportedRuntime)
			}
			if tc.wantReason != "" && !strings.Contains(request.UnsupportedRuntime, tc.wantReason) {
				t.Fatalf("classifier did not retain decoded source disposition %q: %q", tc.wantReason, request.UnsupportedRuntime)
			}
			if request.Env == nil || request.Fingerprint == "" || request.Act == nil || request.Platform == "" {
				t.Fatalf("caller did not build a normal production request: %+v", request)
			}
			if wire, ok := envValue(argv, entrypoint.ProgramCapturesEnv); ok && wire != "{}" {
				t.Fatalf("customized consumer runtime received a sealed capture: %s", wire)
			}
		})
	}
}
