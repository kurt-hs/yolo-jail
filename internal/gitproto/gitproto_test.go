package gitproto

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestIdentity(t *testing.T) {
	for in, want := range map[string]string{
		"git@github.com:o/r.git":           "github.com/o/r",
		"ssh://git@github.com/o/r":         "github.com/o/r",
		"https://github.com/o/r.git":       "github.com/o/r",
		"https://GitHub.com/o/r/":          "github.com/o/r",
		"ssh://git@host:2222/~/repo.git":   "host:2222/repo",
		"https://gitlab.example.com/a/b/c": "gitlab.example.com/a/b/c",
		// Beyond the plan's table: user-info with a password is dropped, an scp-style
		// remote with no user and a home-relative path, and a .git/ suffix.
		"https://me:hunter2@git.example.com/x.git": "git.example.com/x",
		"host.example:~/repo.git":                  "host.example/repo",
		"ssh://git@GitHub.com/o/r.git/":            "github.com/o/r",
	} {
		got, err := Identity(in)
		if err != nil || got != want {
			t.Errorf("Identity(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "file:///x", "/srv/x.git", "ssh://-oProxyCommand=x/r", "git@:r",
		// Beyond the plan's table.
		"http://github.com/o/r", "git://github.com/o/r", "ssh://github.com", "https://github.com/",
		"git@github.com:", "ssh://-oX@github.com/o/r", "-oProxyCommand=x:r", "https://github.com/o/r?x=1",
		"https://github.com/o/r#frag", "https://github.com/o/../x", "https://github.com/o//r",
		"git@github.com:o/r .git", "./relative/x", "[::1]:r", "https://github.com/.git",
	} {
		if got, err := Identity(bad); err == nil {
			t.Errorf("Identity(%q) accepted as %q", bad, got)
		}
	}
}

const zero = "0000000000000000000000000000000000000000"

func TestReadCommands(t *testing.T) {
	var in bytes.Buffer
	w := NewWriter(&in)
	w.Line(zero + " 1111111111111111111111111111111111111111 refs/heads/agent/x\x00report-status side-band-64k push-options\n")
	w.Line("2222222222222222222222222222222222222222 " + zero + " refs/heads/old\n")
	w.Flush()
	w.Line("ci.skip\n")
	w.Flush()
	cmds, caps, opts, err := ReadCommands(NewReader(&in))
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 2 || !cmds[0].Create() || !cmds[1].Delete() || cmds[0].Ref != "refs/heads/agent/x" {
		t.Fatalf("commands %+v", cmds)
	}
	if !caps.Has("side-band-64k") || !caps.Has("push-options") || len(opts) != 1 {
		t.Fatalf("caps %v opts %v", caps, opts)
	}
	if NeedsPack(cmds[1:]) {
		t.Fatal("a delete-only push carries no pack")
	}
}

func TestRefusalReportIsGitsOwnLine(t *testing.T) {
	var out bytes.Buffer
	cmds := []Command{{Old: zero, New: "1111111111111111111111111111111111111111", Ref: "refs/heads/main"}}
	WriteRefusal(&out, Caps{"report-status": true, "side-band-64k": true}, cmds,
		"needs approval, which this version cannot ask for")
	// Captured before ReadReport drains the buffer (the plan's test read out.String() after it,
	// which a bytes.Buffer returns empty).
	raw := out.String()
	report, err := ReadReport(NewReader(&out), true)
	if err != nil {
		t.Fatal(err)
	}
	if report.Unpack != "ok" || report.Refs["refs/heads/main"] != "needs approval, which this version cannot ask for" {
		t.Fatalf("report %+v", report)
	}
	if !strings.Contains(raw, "ng refs/heads/main ") {
		t.Fatalf("no ng line in %q", raw)
	}
}

// The rest are the degenerate cases the plan's "Ships with" names for this package.

func TestEmptyPushHasNoCommands(t *testing.T) {
	in := bytes.NewBufferString("0000")
	cmds, caps, opts, err := ReadCommands(NewReader(in))
	if err != nil || len(cmds) != 0 || len(caps) != 0 || len(opts) != 0 {
		t.Fatalf("empty push: %v %v %v %v", cmds, caps, opts, err)
	}
	if NeedsPack(cmds) {
		t.Fatal("an empty push carries no pack")
	}
}

func TestPushOptionsReadOnlyWhenNegotiated(t *testing.T) {
	var in bytes.Buffer
	w := NewWriter(&in)
	w.Line(zero + " 1111111111111111111111111111111111111111 refs/heads/x\x00report-status\n")
	w.Flush()
	in.WriteString("PACK") // the pack follows directly; nothing may be consumed past the flush
	r := NewReader(&in)
	cmds, _, opts, err := ReadCommands(r)
	if err != nil || len(cmds) != 1 || len(opts) != 0 {
		t.Fatalf("%v %v %v", cmds, opts, err)
	}
	if !NeedsPack(cmds) {
		t.Fatal("a creation carries a pack")
	}
	if rest := in.String(); rest != "PACK" {
		t.Fatalf("ReadCommands consumed the pack: %q left", rest)
	}
}

func TestReadCommandsSkipsShallowLines(t *testing.T) {
	var in bytes.Buffer
	w := NewWriter(&in)
	w.Line("shallow 3333333333333333333333333333333333333333\n")
	w.Line(zero + " 1111111111111111111111111111111111111111 refs/heads/x\x00report-status\n")
	w.Flush()
	cmds, caps, _, err := ReadCommands(NewReader(&in))
	if err != nil || len(cmds) != 1 || !caps.Has("report-status") {
		t.Fatalf("%v %v %v", cmds, caps, err)
	}
}

func TestReadCommandsRefusesGarbage(t *testing.T) {
	for name, lines := range map[string][]string{
		"short id":       {"1234 1111111111111111111111111111111111111111 refs/heads/x\x00report-status\n"},
		"non-hex id":     {"zz" + zero[2:] + " 1111111111111111111111111111111111111111 refs/heads/x\n"},
		"upper-hex id":   {"AA" + zero[2:] + " 1111111111111111111111111111111111111111 refs/heads/x\n"},
		"mixed lengths":  {zero + " " + strings.Repeat("1", 64) + " refs/heads/x\n"},
		"no ref":         {zero + " 1111111111111111111111111111111111111111 \n"},
		"space in ref":   {zero + " 1111111111111111111111111111111111111111 refs/heads/a b\n"},
		"caps twice":     {zero + " 1111111111111111111111111111111111111111 refs/heads/a\x00x\n", zero + " 1111111111111111111111111111111111111111 refs/heads/b\x00y\n"},
		"signed push":    {"push-cert\x00report-status\n"},
		"shallow after":  {zero + " 1111111111111111111111111111111111111111 refs/heads/a\n", "shallow " + zero + "\n"},
		"bad shallow id": {"shallow xyz\n"},
	} {
		var in bytes.Buffer
		w := NewWriter(&in)
		for _, l := range lines {
			w.Line(l)
		}
		w.Flush()
		if cmds, _, _, err := ReadCommands(NewReader(&in)); err == nil {
			t.Errorf("%s: accepted %+v", name, cmds)
		}
	}
	// A stream that ends before its flush is an error, not an empty push.
	if _, _, _, err := ReadCommands(NewReader(strings.NewReader(""))); err == nil {
		t.Error("an empty stream was accepted")
	}
	// A delim inside the command list is not git's grammar.
	if _, _, _, err := ReadCommands(NewReader(strings.NewReader("0001"))); err == nil {
		t.Error("a delim packet was accepted as a command list")
	}
}

func TestSHA256IDs(t *testing.T) {
	z := strings.Repeat("0", 64)
	c := Command{Old: z, New: strings.Repeat("a", 64), Ref: "refs/heads/x"}
	if !c.Create() || c.Delete() {
		t.Fatalf("%+v", c)
	}
}

func TestPktLineSpecialsAndLimits(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	_ = w.Line("a\n")
	_ = w.Delim()
	_ = w.ResponseEnd()
	_ = w.Flush()
	if got := buf.String(); got != "0006a\n000100020000" {
		t.Fatalf("written %q", got)
	}
	r := NewReader(&buf)
	var kinds []Kind
	for range 4 {
		p, err := r.Read()
		if err != nil {
			t.Fatal(err)
		}
		kinds = append(kinds, p.Kind)
	}
	if kinds[0] != Data || kinds[1] != Delim || kinds[2] != ResponseEnd || kinds[3] != Flush {
		t.Fatalf("kinds %v", kinds)
	}
	if err := w.Data(make([]byte, MaxData+1)); err == nil {
		t.Fatal("an over-long payload was written")
	}
	if err := w.Data(make([]byte, MaxData)); err != nil {
		t.Fatalf("a payload at the cap was refused: %v", err)
	}
	for _, in := range []string{"fff1" + strings.Repeat("x", 65521-4), "0003", "zzzz", "000"} {
		if _, err := NewReader(strings.NewReader(in)).Read(); err == nil {
			t.Errorf("read %q without error", in[:min(4, len(in))])
		}
	}
	if _, err := NewReader(strings.NewReader("0008ab")).Read(); err == nil {
		t.Error("a truncated packet was accepted")
	}
}

func TestRefusalWithoutSideband(t *testing.T) {
	var out bytes.Buffer
	cmds := []Command{
		{Old: zero, New: "1111111111111111111111111111111111111111", Ref: "refs/heads/a"},
		{Old: "1111111111111111111111111111111111111111", New: zero, Ref: "refs/notes/x"},
	}
	if err := WriteRefusal(&out, Caps{"report-status-v2": true}, cmds, "multi\nline"); err != nil {
		t.Fatal(err)
	}
	report, err := ReadReport(NewReader(&out), false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Unpack != "ok" || len(report.Refs) != 2 || report.Refs["refs/notes/x"] != "multi line" {
		t.Fatalf("report %+v", report)
	}
	if report.OK("refs/heads/a") {
		t.Fatal("a refused ref reads as ok")
	}
}

func TestRefusalWithoutReportStatusWritesNothing(t *testing.T) {
	var out bytes.Buffer
	cmds := []Command{{Old: zero, New: "1111111111111111111111111111111111111111", Ref: "refs/heads/a"}}
	if err := WriteRefusal(&out, Caps{}, cmds, "no"); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatalf("wrote %q to a client that asked for no report", out.String())
	}
}

// An upstream's report, side-band multiplexed with progress on band 2 and split across
// band-1 packets, as receive-pack writes it.
func TestReadReportDemultiplexes(t *testing.T) {
	var inner bytes.Buffer
	iw := NewWriter(&inner)
	_ = iw.Line("unpack ok\n")
	_ = iw.Line("ok refs/heads/agent/x\n")
	_ = iw.Line("option refname refs/heads/agent/x\n")
	_ = iw.Line("ng refs/heads/main pre-receive hook declined\n")
	_ = iw.Flush()
	var outer bytes.Buffer
	ow := NewWriter(&outer)
	_ = ow.Data(append([]byte{2}, "remote: checking\n"...))
	b := inner.Bytes()
	_ = ow.Data(append([]byte{1}, b[:7]...))
	_ = ow.Data(append([]byte{1}, b[7:]...))
	_ = ow.Flush()
	report, err := ReadReport(NewReader(&outer), true)
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK("refs/heads/agent/x") || report.OK("refs/heads/main") ||
		report.Refs["refs/heads/main"] != "pre-receive hook declined" {
		t.Fatalf("report %+v", report)
	}
	if len(report.Order) != 2 || report.Order[0] != "refs/heads/agent/x" {
		t.Fatalf("order %v", report.Order)
	}

	// Band 3 is a fatal error from the remote.
	var fatal bytes.Buffer
	fw := NewWriter(&fatal)
	_ = fw.Data(append([]byte{3}, "disk full\n"...))
	if _, err := ReadReport(NewReader(&fatal), true); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("band 3: %v", err)
	}
}

func TestReadReportUnpackFailure(t *testing.T) {
	var in bytes.Buffer
	w := NewWriter(&in)
	_ = w.Line("unpack index-pack abnormal exit\n")
	_ = w.Line("ng refs/heads/a unpacker error\n")
	_ = w.Flush()
	report, err := ReadReport(NewReader(&in), false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Unpack != "index-pack abnormal exit" || report.OK("refs/heads/a") {
		t.Fatalf("report %+v", report)
	}
	var bad bytes.Buffer
	bw := NewWriter(&bad)
	_ = bw.Line("ok refs/heads/a\n")
	_ = bw.Flush()
	if _, err := ReadReport(NewReader(&bad), false); err == nil {
		t.Fatal("a report without its unpack line was accepted")
	}
}

func TestSidebandChunksAtTheNegotiatedSize(t *testing.T) {
	var out bytes.Buffer
	if err := WriteSideband(NewWriter(&out), Caps{"side-band": true}, 1, make([]byte, 2000)); err != nil {
		t.Fatal(err)
	}
	r := NewReader(&out)
	total := 0
	for {
		p, err := r.Read()
		if err != nil {
			break
		}
		if len(p.Data) > 999 || p.Data[0] != 1 {
			t.Fatalf("a side-band packet of %d bytes on band %d", len(p.Data), p.Data[0])
		}
		total += len(p.Data) - 1
	}
	if total != 2000 {
		t.Fatalf("carried %d bytes", total)
	}
}

func TestTeeRecordsRawBytes(t *testing.T) {
	var in bytes.Buffer
	w := NewWriter(&in)
	_ = w.Line(zero + " 1111111111111111111111111111111111111111 refs/heads/x\x00report-status\n")
	_ = w.Flush()
	raw := in.String()
	var seen bytes.Buffer
	r := NewReader(&in)
	r.Tee(&seen)
	if _, _, _, err := ReadCommands(r); err != nil {
		t.Fatal(err)
	}
	if seen.String() != raw {
		t.Fatalf("tee saw %q, want %q", seen.String(), raw)
	}
}

func TestErrorsAreProtocolErrors(t *testing.T) {
	_, err := NewReader(strings.NewReader("0003")).Read()
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("%v is not ErrProtocol", err)
	}
}
