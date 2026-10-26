package run

import (
	"path/filepath"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/jailcontent"
	"github.com/mschulkind-oss/yolo-jail/internal/macosuser"
)

// mechanicsHostFileBackings folds the source the host-file emitter actually delivered into
// the fact row. OriginalSource remains separate: Apple Container materializes files, while
// podman may bind either the original or its safe per-launch dereference.
func mechanicsHostFileBackings(facts *jailcontent.FactSheet, entries []config.HostFileEntry, argv []string, rt, wsState string) {
	if facts == nil {
		return
	}
	for _, entry := range entries {
		if !entry.SourceBearing() {
			continue
		}
		target := hostUserCtxDir + "/" + entry.Slug()
		backing := ""
		delivery := "read-only bind to context staging"
		if rt == "container" && !entry.IsDir { // parity: HonoredBy — Apple Container materializes host files in wsState, which its whole-home bind delivers
			if isFile(entry.Source) {
				backing = filepath.Join(wsState, filepath.FromSlash(acCtxDirRel), "host-user", entry.Slug())
				delivery = "copy to per-workspace context staging at launch"
			}
		} else {
			for i := 0; i+1 < len(argv); i++ {
				if argv[i] != "-v" {
					continue
				}
				arg := argv[i+1]
				marker := ":" + target
				if at := strings.LastIndex(arg, marker); at >= 0 {
					backing = arg[:at]
					if backing == entry.Source {
						delivery = "read-only bind from original host source"
					} else {
						delivery = "read-only bind from staged host-source copy"
					}
					break
				}
			}
		}
		if backing == "" {
			delivery = "source absent or declined; host-file fallback layer applies"
		}
		for i := range facts.Paths {
			if facts.Paths[i].Path != filepath.ToSlash(filepath.Join(jailHome, entry.Path)) {
				continue
			}
			facts.Paths[i].OriginalSource = entry.Source
			facts.Paths[i].BackingSource = backing
			facts.Paths[i].HostSource = backing
			facts.Paths[i].Delivery = delivery
		}
	}
}

// mechanicsNativeHostFileBackings records the host files buildMacosCtxTree copied and the
// protected per-session context path StageCtxCommands delivers to the native sandbox.
func mechanicsNativeHostFileBackings(facts *jailcontent.FactSheet, hostCtx macosuser.HostContext, cname string) {
	if facts == nil {
		return
	}
	for _, entry := range hostCtx.HostFiles {
		if !entry.SourceBearing() {
			continue
		}
		staged := filepath.Join(hostCtx.Tree, "host-user", entry.Slug())
		present := hostCtx.Tree != "" && isFile(staged)
		if entry.IsDir {
			present = hostCtx.Tree != "" && isDir(staged)
		}
		destination := filepath.ToSlash(filepath.Clean(entry.Path))
		for i := range facts.Paths {
			path := filepath.ToSlash(facts.Paths[i].Path)
			if facts.Paths[i].OriginalSource != entry.Source || !strings.HasSuffix(path, "/"+destination) {
				continue
			}
			if !present {
				facts.Paths[i].Delivery = "no source copy; host-file layers render at launch"
				continue
			}
			backing := filepath.Join(macosuser.StagedCtxRoot(cname, ""), "host-user", entry.Slug())
			facts.Paths[i].BackingSource = backing
			facts.Paths[i].HostSource = backing
			facts.Paths[i].Lifetime = "per-launch context copy"
			facts.Paths[i].Delivery = "copy into native context staging at launch"
		}
	}
}
