package run

import (
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/durable"
	"github.com/mschulkind-oss/yolo-jail/internal/jailcontent"
)

func TestRenderedMechanicsMapMatchesWritableRunArgv(t *testing.T) {
	for _, rt := range []string{"podman", "container"} {
		t.Run(rt, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			emptyLoopholeDirs(t)
			o := goldenOptions("/ws", home)
			cfg, packs := persistenceFixture(t, "")
			in := relocationInput(t, rt, "/ws/.yolo/home", nil)
			in.cfg, in.packs = cfg, packs
			in.writableHomeDirs = persistenceWritableHomeDirs(cfg, packs)
			in.durableDir = durable.ContainerJailPath
			argv := o.assembleRunCmd(in)
			facts := o.mechanicsFactSheet(in.cname, cfg, rt, stagedPacks{packs: packs}, storePackagesPlan{}, nil,
				persistenceSources{cacheSource: in.cacheSource(), miseSource: in.miseSource(""), scratchID: in.scratchID})
			rendered, err := jailcontent.RenderMechanicsIndexAndMap(*facts)
			if err != nil {
				t.Fatal(err)
			}
			rows := map[string]string{}
			for _, line := range strings.Split(string(rendered["jail-mechanics/host-map.md"]), "\n") {
				if !strings.HasPrefix(line, "| `") {
					continue
				}
				columns := strings.Split(line, "|")
				if len(columns) < 7 {
					continue
				}
				path := strings.Trim(strings.TrimSpace(columns[1]), "`")
				mode := strings.TrimSpace(columns[4])
				if mode == "read-write" {
					rows[path] = strings.Trim(strings.TrimSpace(columns[2]), "`")
				}
			}
			expectedSources := map[string]string{"/workspace": o.Workspace}
			for _, entry := range facts.Persistence.Paths {
				expectedSources[entry.Path] = facts.Persistence.HostSources[entry.Path]
			}
			mounts := writableMountsOf(argv)
			if len(mounts) == 0 {
				t.Fatal("assembled argv contains no parsed writable mounts")
			}
			argvDestinations := map[string]bool{}
			for _, mount := range mounts {
				argvDestinations[mount.dest] = true
			}
			for path, source := range expectedSources {
				if got, ok := rows[path]; !ok {
					t.Errorf("applied map path %s is absent from rendered output", path)
				} else if got != source {
					t.Errorf("rendered original source for %s = %s, fact source = %s", path, got, source)
				}
				if !argvDestinations[path] {
					t.Errorf("fact path %s is absent from assembled argv", path)
				}
			}
			for path, source := range rows {
				if _, ok := expectedSources[path]; !ok {
					t.Errorf("rendered writable path %s -> %s is not in the applied persistence/workspace facts", path, source)
				}
			}
		})
	}
}
