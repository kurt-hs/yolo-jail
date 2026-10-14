package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/reporoot"
)

const preparationOrdinaryProgramKind = "ordinary-program"

// ':' is forbidden in pack names, so an ordinary key cannot collide with a source program/tree key.
func ordinaryProgramKey(in npmCaptureInput) string { return "ordinary:" + in.Owner + "/" + in.Bin }
func ordinaryProgramIdentity(in npmCaptureInput, fingerprint, rt, platform string) string {
	b, _ := json.Marshal(struct {
		Input                          npmCaptureInput
		Fingerprint, Runtime, Platform string
	}{in, fingerprint, rt, platform})
	return npmCaptureHash(b)
}

func ordinarySelectionEnvironment(cfg *jsonx.OrderedMap, packs []*packload.Pack) *entrypoint.Env {
	vars := map[string]string{}
	for key, env := range map[string]string{"mcp_presets": "YOLO_MCP_PRESETS", "mise_tools": "YOLO_MISE_TOOLS"} {
		v, ok := cfg.Get(key)
		if ok {
			vars[env] = jsonx.MustDumpsCompact(v)
		}
	}
	raw, _ := cfg.Get("mcp_servers")
	table, _ := raw.(*jsonx.OrderedMap)
	vars["YOLO_MCP_SERVERS"] = jsonx.MustDumpsCompact(packload.ComposeMCPServers(table, packs, "/home/agent"))
	return entrypoint.NewEnv(vars)
}

func selectedOrdinaryInputs(e *entrypoint.Env, packs []*packload.Pack, mode entrypoint.ProgramPreparationMode) ([]npmCaptureInput, error) {
	selected, err := entrypoint.SelectedProgramPreparations(e, packs, mode)
	if err != nil {
		return nil, err
	}
	selected = append(selected, entrypoint.SelectedServerPreparations(e, packs, mode)...)
	seen := map[string]bool{}
	var out []npmCaptureInput
	for _, p := range selected {
		in, ok := npmCaptureInputFor(p.Pack, p.Install)
		if !ok || seen[in.Bin] {
			continue
		}
		seen[in.Bin] = true
		out = append(out, in)
	}
	return out, nil
}

type resolvedPreparationDemand struct {
	Fork        packload.Fork
	NPM         *npmCaptureInput
	Fingerprint string
}

func resolvePreparationDemand(ctx context.Context, d preparationDemand) (resolvedPreparationDemand, func(), error) {
	if d.Kind == preparationOrdinaryProgramKind {
		in, fingerprint, release, err := resolveOrdinaryPreparation(ctx, d)
		return resolvedPreparationDemand{NPM: in, Fingerprint: fingerprint}, release, err
	}
	f, release, err := resolveDemandFork(ctx, d)
	return resolvedPreparationDemand{Fork: f}, release, err
}

// resolveOrdinaryPreparation obtains INSTALLATION authority only from current user
// declarations. The requester carries key/identity, never executable paths or a manifest.
func resolveOrdinaryPreparation(ctx context.Context, d preparationDemand) (*npmCaptureInput, string, func(), error) {
	resolver, release := preparationScopedResolver(ctx)
	var contention error
	selected := selectHostPacks(func(entry config.PackEntry) (*packload.Pack, error) {
		p, err := resolver(entry)
		if errors.Is(err, packsrc.ErrLockHeld) {
			contention = err
		}
		return p, err
	}, config.UserScopeSelection())
	if contention != nil {
		return nil, "", release, contention
	}
	if problems := selected.problems(); len(problems) > 0 {
		return nil, "", release, fmt.Errorf("current user selection is incomplete — repair the user config before retrying preparation")
	}
	cfg, err := config.UserScopeConfig(true, nil)
	if err != nil {
		return nil, "", release, err
	}
	if run.OrdinaryRuntimeUnsupported(cfg, os.Getenv("YOLO_STORE_PACKAGES") == "1") != "" {
		return nil, "", release, errSharedPreparationForegroundOnly
	}
	root, ok := reporoot.Resolve(os.Getenv)
	if !ok {
		return nil, "", release, fmt.Errorf("ordinary runtime source is unavailable — run yolo check to repair its flake source")
	}
	fingerprint, err := run.ProgramRuntimeFingerprint(root.Root, cfg)
	if err != nil {
		return nil, "", release, err
	}
	vector, err := selectedOrdinaryInputs(ordinarySelectionEnvironment(cfg, selected.packs), selected.packs, entrypoint.ProgramInstallOnly)
	if err != nil {
		return nil, "", release, err
	}
	for _, in := range vector {
		if ordinaryProgramKey(in) != d.Key {
			continue
		}
		identity := ordinaryProgramIdentity(in, fingerprint, d.Runtime, d.Platform)
		if d.Identity != "" && identity != d.Identity {
			return nil, "", release, errPreparationAdmissionStale
		}
		if d.Platform != captureJailPlatform() || !strings.HasPrefix(d.Platform, "linux/") {
			return nil, "", release, errSharedPreparationForegroundOnly
		}
		if d.Background && !entrypoint.PackPolicyAllows(config.AgentUpdatesWire(), in.Owner) {
			return nil, "", release, errPreparationAdmissionStale
		}
		if d.SubscriptionGeneration != 0 {
			state, e := loadPreparationState()
			if e != nil {
				return nil, "", release, e
			}
			idx := preparationRecordIndex(state, d.Key, d.Runtime, d.Platform)
			if idx < 0 || !state.Subscriptions[idx].Active || state.Subscriptions[idx].Generation != d.SubscriptionGeneration || state.Subscriptions[idx].InputIdentity != identity {
				return nil, "", release, errPreparationAdmissionStale
			}
		}
		return &in, fingerprint, release, nil
	}
	return nil, "", release, errSharedPreparationForegroundOnly
}

func preparationDemandInputIdentity(resolved resolvedPreparationDemand, d preparationDemand) (string, error) {
	if resolved.NPM != nil {
		return ordinaryProgramIdentity(*resolved.NPM, resolved.Fingerprint, d.Runtime, d.Platform), nil
	}
	return preparationInputIdentity(resolved.Fork, d.Runtime, d.Platform)
}

func ordinaryCaptureDelivery(r npmCaptureRecord) (entrypoint.ProgramCaptureDelivery, error) {
	if r.Resolution == nil {
		return entrypoint.ProgramCaptureDelivery{}, fmt.Errorf("ordinary preparation completed without resolved facts — retry a fresh launch")
	}
	inputs, err := r.Resolution.inputs()
	if err != nil {
		return entrypoint.ProgramCaptureDelivery{}, err
	}
	identity, err := inputs.Identity()
	if err != nil {
		return entrypoint.ProgramCaptureDelivery{}, err
	}
	if identity != r.Identity {
		return entrypoint.ProgramCaptureDelivery{}, errPreparationAdmissionStale
	}
	return entrypoint.ProgramCaptureDelivery{Declared: inputs.Declared, Compatibility: identity, Platform: inputs.Platform, Runtime: inputs.Runtime, NodeFloor: r.Resolution.Input.NodeFloor}, nil
}

func waitOrdinaryPreparation(ctx context.Context, in npmCaptureInput, fingerprint, rt, platform string, fresh bool, w io.Writer, foregroundGuard func(func() error) error) (npmCaptureRecord, error) {
	store := &capture.Store{Dir: paths.CapturesDir()}
	d := preparationDemand{Key: ordinaryProgramKey(in), Kind: preparationOrdinaryProgramKind, Runtime: rt, Platform: platform, Identity: ordinaryProgramIdentity(in, fingerprint, rt, platform), Fresh: fresh}
	probe := d
	probe.Identity = ""
	current, currentFingerprint, release, err := resolveOrdinaryPreparation(ctx, probe)
	release()
	foreground := config.InJail() || errors.Is(err, errSharedPreparationForegroundOnly) || err == nil && (current == nil || *current != in || currentFingerprint != fingerprint)
	if err != nil && !foreground {
		return npmCaptureRecord{}, err
	}
	if !fresh {
		record, readErr := readNPMCaptureRecord(store, in)
		if readErr == nil && record.Fingerprint == fingerprint && record.Resolution != nil && record.Resolution.Runtime.Platform == platform {
			entry, _, hitErr := resolveProgramCaptureFor(store, in.Bin, platform, record.Identity)
			if hitErr == nil {
				manifest, manifestErr := capture.ReadManifest(entry.Root)
				if manifestErr == nil {
					manifestErr = validateProgramManifest(manifest, platform, npmCaptureArtifacts(in, record.Identity))
				}
				if manifestErr != nil {
					return record, manifestErr
				}
				if err = foregroundGuard(func() error { return ctx.Err() }); err != nil {
					return record, err
				}
				return record, nil
			}
		}
	}
	boundary, err := registerNPMCaptureRequest(ctx, store, in)
	if err != nil {
		return npmCaptureRecord{}, err
	}
	d.AfterSeq = boundary
	if foreground {
		fmt.Fprintf(w, "%s: foreground-only installation preparation; the user resolver does not authorize these exact inputs for a detached worker\n", in.Bin)
		return advanceNPMCapture(withOrdinaryCaptureRuntime(ctx, rt), store, in, fingerprint, fresh, boundary, w, foregroundGuard)
	}
	result, err := waitSharedDemandResult(ctx, d, false, func(pid int) {
		fmt.Fprintf(w, "%s: waiting for shared ordinary preparation (pid %d); interrupting detaches this requester\n", in.Bin, pid)
	})
	if err != nil {
		return npmCaptureRecord{}, err
	}
	if result.UnsupportedNPM {
		return npmCaptureRecord{}, fmt.Errorf("%w: %s", errNPMCaptureUnsupported, result.OperationError)
	}
	if result.Failed || result.OperationError != "" {
		return npmCaptureRecord{}, fmt.Errorf("ordinary preparation failed: %s — retry a fresh launch", result.OperationError)
	}
	if result.Program == nil {
		return npmCaptureRecord{}, fmt.Errorf("ordinary worker returned no admitted generation — retry preparation")
	}
	if err = foregroundGuard(func() error { return ctx.Err() }); err != nil {
		return npmCaptureRecord{}, err
	}
	if fresh && result.Program.Completed <= boundary {
		return npmCaptureRecord{}, fmt.Errorf("ordinary check predates the request — retry --update")
	}
	return *result.Program, nil
}

func prepareOrdinaryForLaunch(req run.ProgramCaptureRequest, w io.Writer) (map[string]entrypoint.ProgramCaptureDelivery, error) {
	if req.UnsupportedRuntime != "" {
		fmt.Fprintf(w, "ordinary artifact reuse is unsupported here: %s; retaining the authorized bounded installers — use a baseline runtime without that customization for sealed reuse\n", req.UnsupportedRuntime)
		return map[string]entrypoint.ProgramCaptureDelivery{}, nil
	}
	mode := entrypoint.ProgramInstallOnly
	if req.Fresh {
		mode = entrypoint.ProgramFreshRefresh
	}
	vector, err := selectedOrdinaryInputs(req.Env, req.Packs, mode)
	if err != nil {
		return nil, err
	}
	deliveries := map[string]entrypoint.ProgramCaptureDelivery{}
	if len(vector) == 0 {
		return deliveries, nil
	}
	var operationErr error
	req.Act.Scope(func(ctx context.Context) {
		if err := registerOrdinaryPreparations(ctx, vector, req, w); err != nil {
			fmt.Fprintf(w, "ordinary timer preparation could not start (%v) — this launch still prepares its required artifacts; run a fresh launch to retry the timer\n", err)
		}
		for _, in := range vector {
			guard := foregroundOrdinaryGuard(ctx, req, in)
			record, err := waitOrdinaryPreparation(ctx, in, req.Fingerprint, req.Runtime, req.Platform, req.Fresh, w, guard)
			if errors.Is(err, errNPMCaptureUnsupported) {
				fmt.Fprintf(w, "%s: sealed dependency capture is unsupported (%v); retaining its bounded declared installer — use an entirely locked registry dependency graph for sealed reuse\n", in.Bin, err)
				continue
			}
			if err != nil {
				operationErr = err
				return
			}
			delivery, err := ordinaryCaptureDelivery(record)
			if err != nil {
				operationErr = err
				return
			}
			deliveries[in.Bin] = delivery
		}
	})
	if req.Act.Interrupted() || operationErr != nil {
		if operationErr == nil {
			operationErr = context.Canceled
		}
		return nil, operationErr
	}
	return deliveries, nil
}

func foregroundOrdinaryGuard(ctx context.Context, req run.ProgramCaptureRequest, in npmCaptureInput) func(func() error) error {
	return func(commit func() error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// Foreground authority is the launch's live selection. Re-read its config and
		// runtime recipe rather than converting unresolved workspace inputs into a daemon.
		cfg, err := config.LoadConfig(req.Workspace, true, nil)
		if err != nil {
			return err
		}
		if run.OrdinaryRuntimeUnsupported(cfg, os.Getenv("YOLO_STORE_PACKAGES") == "1") != "" {
			return errPreparationAdmissionStale
		}
		fingerprint, err := run.ProgramRuntimeFingerprint(req.RepoRoot, cfg)
		if err != nil || fingerprint != req.Fingerprint {
			return errPreparationAdmissionStale
		}
		resolver, release := preparationScopedResolver(ctx)
		defer release()
		selected := selectHostPacks(resolver, config.UserScopeSelection())
		if len(selected.problems()) > 0 {
			return errPreparationAdmissionStale
		}
		vector, err := selectedOrdinaryInputs(ordinarySelectionEnvironment(cfg, selected.packs), selected.packs, entrypoint.ProgramInstallOnly)
		if err != nil {
			return err
		}
		for _, current := range vector {
			if current == in {
				return commit()
			}
		}
		return errPreparationAdmissionStale
	}
}
