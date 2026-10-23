package macosuser

// loopbackports_test.go pins slice 7 of docs/design/macos-user-model-plan.md (MMP-D10): the
// loopback allow list's three sources, its order, the refusal for an endpoint whose port cannot be
// read, and the CALL SITE — the orchestrator collects it and the run-as-you profile renders it.

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/svcendpoint"
)

// claudeBrokerVar is the claude OAuth broker's endpoint variable, as the run pipeline spells it.
var claudeBrokerVar = paths.ServiceEnvVarPrefix + "CLAUDE_OAUTH_BROKER" + paths.ServiceEnvVarSuffix

// publishEndpoint publishes a well-formed endpoint file advertising hostPort in a private
// directory and returns its path. The certificate and token come from a real listener, so the
// file is one svcendpoint.Read accepts.
func publishEndpoint(t *testing.T, name, hostPort string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "services")
	src := filepath.Join(dir, "seed-"+name)
	ln, err := svcendpoint.Listen(src, "")
	if err != nil {
		t.Fatal(err)
	}
	ep, err := svcendpoint.Read(src)
	_ = ln.Close()
	if err != nil {
		t.Fatal(err)
	}
	ep.HostPort = hostPort
	p := filepath.Join(dir, name+paths.ServiceEndpointExt)
	if err := svcendpoint.Publish(p, ep); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoopbackPortsReadsEveryPublishedEndpoint(t *testing.T) {
	env := jsonx.NewOrderedMap()
	env.Set("YOLO_SERVICE_B_ENDPOINT", publishEndpoint(t, "b", "127.0.0.1:40002"))
	env.Set("YOLO_SERVICE_A_ENDPOINT", publishEndpoint(t, "a", "127.0.0.1:40001"))
	env.Set("UNRELATED", "127.0.0.1:1")
	got, err := LoopbackPorts([]string{"127.0.0.1:51000"}, []any{"11434:11435"}, env)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{11434, 40001, 40002, 51000}; !reflect.DeepEqual(got, want) {
		t.Errorf("LoopbackPorts = %v, want %v (sorted, unique)", got, want)
	}
	// Unique across sources and envs, and the jail daemons' env counts too.
	daemons := jsonx.NewOrderedMap()
	daemons.Set("YOLO_SERVICE_C_ENDPOINT", publishEndpoint(t, "c", "127.0.0.1:40001"))
	got, err = LoopbackPorts([]string{"127.0.0.1:51000", "127.0.0.1:40002"}, []any{"40001", 11434},
		env, daemons)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{11434, 40001, 40002, 51000}; !reflect.DeepEqual(got, want) {
		t.Errorf("LoopbackPorts over overlapping sources = %v, want %v", got, want)
	}
}

func TestLoopbackPortsNamesAMissingEndpoint(t *testing.T) {
	env := jsonx.NewOrderedMap()
	env.Set("YOLO_SERVICE_GONE_ENDPOINT", filepath.Join(t.TempDir(), "gone.endpoint"))
	_, err := LoopbackPorts(nil, nil, env)
	if err == nil || !strings.Contains(err.Error(), "YOLO_SERVICE_GONE_ENDPOINT") {
		t.Errorf("a missing endpoint's error does not name its variable: %v", err)
	}
}

// selfPlanWithLoopback is a run-as-you plan built from a collected allow list.
func selfPlanWithLoopback(t *testing.T, c LoopbackCollection) RunPlan {
	t.Helper()
	return BuildRunPlanWithDaemons("/Users/you/code/alpha", jsonx.NewOrderedMap(), nil, []string{"claude"},
		"/usr/local/bin/yolo", "", HomeOverlay{}, HostContext{}, jsonx.NewOrderedMap(), nil, nil,
		JailDaemons{}, FloorStage{}, PlanSession{Mode: ModeSelf, HostHome: "/Users/you", HostUser: "you",
			DarwinUserTempDir: "/private/var/folders/ab/xyz/T/", Loopback: c})
}

func TestSelfModeProfileOpensTheCollectedPorts(t *testing.T) {
	plan := selfPlanWithLoopback(t, CollectLoopback([]string{"127.0.0.1:51000"}, []any{"11434:11435"}))
	for _, want := range []string{`(remote ip "localhost:11434")`, `(remote ip "localhost:51000")`} {
		if !strings.Contains(plan.Seatbelt, want) {
			t.Errorf("the self-mode profile does not open %s", want)
		}
	}
	if strings.Contains(plan.Seatbelt, "localhost:11435") {
		t.Error("the profile opened a forward's HOST port; only its JAIL port is the sandbox's to dial")
	}
	if plan.Loopback == nil || !reflect.DeepEqual(plan.Loopback.Ports(), []int{11434, 51000}) {
		t.Errorf("the plan does not record the allow list it rendered: %+v", plan.Loopback)
	}
	if problems := PlanInvariants(plan); len(problems) != 0 {
		t.Errorf("problems:\n  %s", strings.Join(problems, "\n  "))
	}
}

func TestAnUnreadEndpointRefusesALiveSelfModePlanAndNotADryRun(t *testing.T) {
	env := jsonx.NewOrderedMap()
	env.Set("YOLO_SERVICE_GONE_ENDPOINT", filepath.Join(t.TempDir(), "gone.endpoint"))
	live := CollectLoopback(nil, nil, env)
	problems := PlanInvariants(selfPlanWithLoopback(t, live))
	if !hasProblem(problems, "YOLO_SERVICE_GONE_ENDPOINT") || !hasProblem(problems, "run `yolo` again") {
		t.Errorf("an unread endpoint is not a problem naming it and the next step:\n  %s",
			strings.Join(problems, "\n  "))
	}
	dry := live
	dry.AtLaunch = true
	plan := selfPlanWithLoopback(t, dry)
	if problems := PlanInvariants(plan); hasProblem(problems, "YOLO_SERVICE_GONE_ENDPOINT") {
		t.Errorf("a dry run's placeholder endpoint was refused:\n  %s", strings.Join(problems, "\n  "))
	}
	if lines := strings.Join(loopbackDisclosure(*plan.Loopback), "\n"); !strings.Contains(lines, "YOLO_SERVICE_GONE_ENDPOINT") {
		t.Errorf("a dry run does not name the port read at launch:\n%s", lines)
	}
}

func TestAnAllowedLoopbackRendersNoPortAndDisclosesNone(t *testing.T) {
	saved := productionLoopback
	t.Cleanup(func() { productionLoopback = saved })
	productionLoopback = LoopbackPolicy{AllowAll: true}
	env := jsonx.NewOrderedMap()
	env.Set("YOLO_SERVICE_GONE_ENDPOINT", filepath.Join(t.TempDir(), "gone.endpoint"))
	plan := selfPlanWithLoopback(t, CollectLoopback([]string{"127.0.0.1:51000"}, nil, env))
	if strings.Contains(plan.Seatbelt, "localhost:51000") || plan.Loopback != nil {
		t.Error("a loopback the ruling allows still renders or records an allow list")
	}
	if problems := PlanInvariants(plan); hasProblem(problems, "YOLO_SERVICE_GONE_ENDPOINT") {
		t.Errorf("an allowed loopback refused an unread endpoint:\n  %s", strings.Join(problems, "\n  "))
	}
}

func TestAccountModeProfileIsUntouchedByTheAllowList(t *testing.T) {
	plan := BuildRunPlanWithDaemons("/Users/Shared/proj", jsonx.NewOrderedMap(), nil, []string{"claude"},
		"/usr/local/bin/yolo", "", HomeOverlay{}, HostContext{}, jsonx.NewOrderedMap(), nil, nil,
		JailDaemons{}, FloorStage{}, PlanSession{Loopback: CollectLoopback([]string{"127.0.0.1:51000"}, nil)})
	if plan.Loopback != nil || strings.Contains(plan.Seatbelt, "localhost:51000") {
		t.Error("the account mode's profile, which does not deny loopback yet, rendered an allow list")
	}
}

// THE CALL SITE: buildPlan collects the list from the launch's own inputs — the claude broker's
// published endpoint on the session env, a served address on JailDaemons.Served and the
// workspace's forward_host_ports — and the run-as-you profile opens each port. Deleting the
// collection from buildPlan, or the Loopback field from the self branch, fails this.
func TestBuildPlanOpensTheClaudeBrokersPortInTheSelfModeProfile(t *testing.T) {
	broker := publishEndpoint(t, "claude-oauth-broker", "127.0.0.1:43117")
	opts := newOpts("/Users/you/code/alpha")
	opts.Mode, opts.HostHome, opts.HostUser = ModeSelf, "/Users/you", "you"
	opts.PackEnv = jsonx.NewOrderedMap()
	opts.PackEnv.Set(claudeBrokerVar, broker)
	opts.JailDaemons.Served = []string{"127.0.0.1:52001"}
	network := jsonx.NewOrderedMap()
	network.Set("forward_host_ports", []any{"11434:11435"})
	opts.Config.Set("network", network)
	plan := buildPlan(mockDeps(nil), opts, nil)
	for _, port := range []string{"43117", "52001", "11434"} {
		if !strings.Contains(plan.Seatbelt, `(remote ip "localhost:`+port+`")`) {
			t.Errorf("the self-mode profile does not open port %s", port)
		}
	}
	lines := strings.Join(loopbackDisclosure(*plan.Loopback), "\n")
	if !strings.Contains(lines, "port 43117") || !strings.Contains(lines, claudeBrokerVar) {
		t.Errorf("the disclosure does not name the broker's port and why:\n%s", lines)
	}
}
