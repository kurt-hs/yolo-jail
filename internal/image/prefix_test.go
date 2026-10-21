package image

import "testing"

func TestJailPrefixSubdirMirrorsTheJailDestination(t *testing.T) {
	if JailPrefixSubdir != "opt/yolo-jail" {
		t.Errorf("JailPrefixSubdir = %q, want opt/yolo-jail (flake.nix installPrefix lays it down there)",
			JailPrefixSubdir)
	}
}
