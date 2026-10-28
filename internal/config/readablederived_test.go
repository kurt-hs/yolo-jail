package config

import (
	"reflect"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

func TestEffectivePackagesReadsCanonicalInstallAndMachineGPUWithoutMutation(t *testing.T) {
	config := jsonx.NewOrderedMap()
	install := jsonx.NewOrderedMap()
	linuxOnly := jsonx.NewOrderedMap()
	linuxOnly.Set("name", "mesa")
	linuxOnly.Set("outputs", []any{"lib", "dev"})
	linuxOnly.Set("platforms", []any{"linux"})
	darwinOnly := jsonx.NewOrderedMap()
	darwinOnly.Set("name", "darwin-tool")
	darwinOnly.Set("platforms", []any{"darwin"})
	install.Set("nix", []any{"jq", linuxOnly, darwinOnly})
	config.Set("install", install)

	machine := jsonx.NewOrderedMap()
	gpu := jsonx.NewOrderedMap()
	gpu.Set("enabled", true)
	gpu.Set("vendor", "amd")
	gpu.Set("vaapi", true)
	machine.Set("gpu", gpu)
	config.Set("machine", machine)

	before, err := SnapshotJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	got := EffectivePackages(config, PlatformLinux)
	if len(got) != 4 || !reflect.DeepEqual(got[0], "jq") || got[1] != linuxOnly ||
		!reflect.DeepEqual(got[2:], []any{"mesa", "libva-utils"}) {
		t.Fatalf("effective canonical Linux packages = %#v, want original rich entries plus AMD VAAPI extras", got)
	}
	if got := EffectivePackages(config, PlatformDarwin); len(got) != 4 ||
		!reflect.DeepEqual(got[0], "jq") || got[1] != darwinOnly ||
		!reflect.DeepEqual(got[2:], []any{"mesa", "libva-utils"}) {
		t.Fatalf("effective canonical Darwin packages = %#v, want Darwin-filtered entries plus AMD VAAPI extras", got)
	}
	after, err := SnapshotJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("EffectivePackages mutated canonical input:\nbefore %s\nafter  %s", before, after)
	}
}
