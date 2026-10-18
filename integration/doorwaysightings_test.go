package integration

import (
	"strings"
	"testing"
	"time"
)

// doorwaysightings_test.go pins doorwayProblems, the judgement every macos-user doorway test
// (macosuserdoorway_test.go, macosuserbridge_test.go, macosuserviaroute_test.go) makes about the
// host process its launch opened. It runs under -short, on Linux: the judgement reads a process
// listing and the launch's output, both handed in here.

// doorwayLaunchOutput is a launch's output naming its AWS doorway, as a keeper at macos-user
// prints it (startMacosUserDoorways), relayed to the launch's stderr.
const doorwayLaunchOutput = `keeper: started, pid 81000
Opened the "aws-auth" doorway (pack "aws-auth", pid 81099) on 127.0.0.1:53111 for this launch, outside the sandbox: it serves every macos-user session of this workspace
`

// TestADoorwayOfAnotherSessionIsNotThisSessionsSurvivor is §6.2 of
// docs/design/macos-user-suite-on-a-developer-mac.md: on a developer Mac another session (the
// developer's own `yolo host -- claude` on Bedrock) holds an aws-credential-adapter doorway with
// the same argv as the one this launch opened. This launch's doorway is gone, so nothing
// survives the session; the check that matched the argv machine-wide failed here.
func TestADoorwayOfAnotherSessionIsNotThisSessionsSurvivor(t *testing.T) {
	foreign := "501 81026 /usr/local/bin/yolo " + awsDoorwayArgv + " 127.0.0.1:50999"
	ours := "501 81099 /tmp/yolo-integration-1/yolo " + awsDoorwayArgv + " 127.0.0.1:53111"
	saw := sightingsIn(foreign+"\n"+ours+"\n", awsDoorwayArgv)
	afterwards := func() string { return "  1 1 /sbin/launchd\n" + foreign + "\n" }

	got := doorwayProblems(awsDoorwayArgv, saw, doorwayLaunchOutput, "501", "299", afterwards, 0, time.Millisecond)
	if len(got) != 0 {
		t.Fatalf("another session's doorway, with the same argv, was judged as this launch's: %q", got)
	}
}

// TestThisLaunchsSurvivingDoorwayIsNamed: the launch's own doorway still running once the launch
// returned is the defect the check exists for, and the message names it and the other one apart.
func TestThisLaunchsSurvivingDoorwayIsNamed(t *testing.T) {
	foreign := "501 81026 /usr/local/bin/yolo " + awsDoorwayArgv + " 127.0.0.1:50999"
	ours := "501 81099 /tmp/yolo-integration-1/yolo " + awsDoorwayArgv + " 127.0.0.1:53111"
	listing := func() string { return foreign + "\n" + ours + "\n" }
	saw := sightingsIn(listing(), awsDoorwayArgv)

	got := doorwayProblems(awsDoorwayArgv, saw, doorwayLaunchOutput, "501", "299", listing, 0, time.Millisecond)
	if len(got) != 1 || !strings.Contains(got[0], "survives the session") || !strings.Contains(got[0], "501 81099") ||
		!strings.Contains(got[0], "not this launch's, so not judged: "+foreign) {
		t.Fatalf("want one survivor problem naming pid 81099 and setting 81026 apart, got %q", got)
	}
}

// TestThisLaunchsDoorwayMustRunAsTheHostUser: the uid the host saw is judged on this launch's
// process alone, and a launch whose doorway was never seen is a problem even when another
// session's was.
func TestThisLaunchsDoorwayMustRunAsTheHostUser(t *testing.T) {
	gone := func() string { return "" }
	inSandbox := sightingsIn("299 81099 yolo "+awsDoorwayArgv+" 127.0.0.1:53111\n", awsDoorwayArgv)
	got := doorwayProblems(awsDoorwayArgv, inSandbox, doorwayLaunchOutput, "501", "299", gone, 0, time.Millisecond)
	if len(got) != 1 || !strings.Contains(got[0], "ran as uid 299") {
		t.Errorf("a doorway of this launch's running as the sandbox account: got %q", got)
	}

	onlyForeign := sightingsIn("501 81026 yolo "+awsDoorwayArgv+" 127.0.0.1:50999\n", awsDoorwayArgv)
	got = doorwayProblems(awsDoorwayArgv, onlyForeign, doorwayLaunchOutput, "501", "299", gone, 0, time.Millisecond)
	if len(got) != 1 || !strings.Contains(got[0], "saw no") || !strings.Contains(got[0], "81099") {
		t.Errorf("only another session's doorway seen: got %q", got)
	}

	got = doorwayProblems(awsDoorwayArgv, onlyForeign, "no start line here\n", "501", "299", gone, 0, time.Millisecond)
	if len(got) != 1 || !strings.Contains(got[0], "names no pid") {
		t.Errorf("a launch output naming no pid: got %q", got)
	}
}

// TestLaunchOwnedPIDsReadsEveryStartLine: the three lines a launch names a host process it
// started by, with any run of whitespace between the words after the name.
func TestLaunchOwnedPIDsReadsEveryStartLine(t *testing.T) {
	out := "Opened the \"openai-auth-broker\" doorway (pack \"openai-auth\",\n  pid 101) on 127.0.0.1:1\n" +
		"Started the \"wire-bridge\" service (pack \"wire-bridge\", pid 202) on 127.0.0.1:2 for this launch\n" +
		"yolo: the \"wire-bridge\" service is back (pid 303) on 127.0.0.1:2 for claude.\n" +
		"keeper: started, pid 404\n"
	got := launchOwnedPIDs(out)
	for _, pid := range []string{"101", "202", "303"} {
		if !got[pid] {
			t.Errorf("pid %s is not read from %q (got %v)", pid, out, got)
		}
	}
	if got["404"] || len(got) != 3 {
		t.Errorf("a pid on another line was read as a launch-owned process: %v", got)
	}
}
