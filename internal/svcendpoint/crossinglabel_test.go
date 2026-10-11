package svcendpoint

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// A LABELLED FRONT STAMPS ITS LABEL ON EVERY CROSSING, accepted and rejected alike: the launch's
// container name and workspace code are what crossings.log names the jail by
// (internal/crossaudit), and a rejected crossing is the one whose jail a reader most needs.
func TestALabelledFrontStampsItsLabelOnEveryCrossing(t *testing.T) {
	rec := captureCrossings(t)
	label := CrossingLabel{Container: "yolo-myproj-1a2b3c4d", Workspace: "9f8e7d6c"}
	endpoint, _ := startEchoFrontWith(t, FrontOptions{Label: label})
	rec.scopeTo(filepath.Dir(endpoint))

	conn, err := Dial(endpoint, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	if _, err := conn.Read(buf); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	if got := rec.await(t, 1)[0]; got.Label != label || got.Outcome != CrossingAccepted {
		t.Errorf("accepted crossing = %+v, want label %+v", got, label)
	}

	ep, err := Read(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	wrong, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(privateDir(t), "wrongtoken.endpoint")
	if err := Publish(bad, Endpoint{HostPort: ep.HostPort, CertDER: ep.CertDER, Token: wrong}); err != nil {
		t.Fatal(err)
	}
	if _, err := Dial(bad, 5*time.Second); !errors.Is(err, ErrAuthRejected) {
		t.Fatalf("Dial with the wrong token: %v, want ErrAuthRejected", err)
	}
	got := rec.await(t, 2)[1]
	if got.Outcome != CrossingRejected || got.Label != label {
		t.Errorf("rejected crossing = %+v, want label %+v", got, label)
	}
}
