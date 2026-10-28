package config

import (
	"fmt"
	"io/fs"
	"net/url"
	"path/filepath"

	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func (ctx *editValidationContext) selectedPacks() ([]*packload.Pack, error) {
	entries, problems := lowerReadablePackEntries(ctx.rawUser, ctx.userOrigins)
	if len(problems) != 0 {
		return nil, fmt.Errorf("read planned pack entries: %v", problems)
	}
	entries = appendImplicitLocalPack(entries)
	fsys := packload.EmbeddedFS()
	embedded := map[string]*packload.Pack{}
	var embeddedErr error
	lookup := func(name string) (*packload.Pack, bool) {
		if pack, ok := embedded[name]; ok {
			return pack, true
		}
		if fsys == nil {
			embeddedErr = fmt.Errorf("embedded pack source is unavailable for %s", name)
			return nil, false
		}
		info, err := fs.Stat(fsys, name)
		if err != nil || !info.IsDir() {
			embeddedErr = fmt.Errorf("embedded pack %s is not present in the immutable source", name)
			return nil, false
		}
		root := filepath.Join(string(filepath.Separator), "embedded", name)
		pack, packProblems, err := packload.ReadSelectedPackFS(fsys, name, root, name, nil, nil)
		if err != nil {
			embeddedErr = fmt.Errorf("read embedded pack %s: %w", name, err)
			return nil, false
		}
		if len(packProblems) != 0 {
			embeddedErr = fmt.Errorf("embedded pack %s has declaration problems: %v", name, packProblems)
			return nil, false
		}
		pack.Official = true
		embedded[name] = pack
		return pack, true
	}
	resolve := func(entry PackEntry) (*packload.Pack, error) {
		var pack *packload.Pack
		var problems []string
		var err error
		if entry.Embedded() {
			if fsys == nil {
				return nil, fmt.Errorf("embedded pack source is unavailable for %s", entry.Name)
			}
			sourceName := entry.EmbeddedName
			if sourceName == "" {
				sourceName = entry.Name
			}
			root := filepath.Join(string(filepath.Separator), "embedded", sourceName)
			pack, problems, err = packload.ReadSelectedPackFS(fsys, sourceName, root, entry.Name, entry.Only, entry.Exclude)
			if err != nil {
				return nil, err
			}
			pack.Official = true
		} else {
			var root string
			if entry.IsLocal() {
				root, err = selectedPackRoot(entry)
			} else {
				root, err = selectedFetchedPackRoot(entry)
			}
			if err != nil {
				return nil, err
			}
			pack, problems, err = packload.ReadSelectedPackPath(root, entry.Name, entry.Only, entry.Exclude, entry.IsLocal())
			if err != nil {
				return nil, err
			}
			pack.Local = entry.IsLocal()
		}
		if len(problems) != 0 {
			return nil, fmt.Errorf("pack %s declaration problems: %v", entry.Name, problems)
		}
		return pack, nil
	}
	selection := packload.Selection{
		UseProfiles: func(set []*packload.Pack) map[string]string {
			return ConfigProfileTable(ctx.rawUser, set)
		},
		UserProfiles: func() (map[string]packload.UserProfile, error) {
			value, present := ctx.rawUser.Get("profiles")
			if !present || value == nil {
				return map[string]packload.UserProfile{}, nil
			}
			profiles, problems := checkProfiles(value)
			if len(problems) != 0 {
				return nil, fmt.Errorf("user profiles: %v", problems)
			}
			return profiles, nil
		},
	}
	selected, err := selectPacksWithEmbeddedLookup(entries, PackSelectSpec{Resolve: resolve, FailFast: true, Selection: selection}, lookup)
	if err != nil {
		if embeddedErr != nil {
			return nil, fmt.Errorf("planned pack closure source: %w (selection: %v)", embeddedErr, err)
		}
		return nil, fmt.Errorf("resolve planned pack selection: %w", err)
	}
	if embeddedErr != nil {
		return nil, fmt.Errorf("planned pack closure source: %w", embeddedErr)
	}
	if !selected.Complete() {
		return nil, fmt.Errorf("planned pack selection is incomplete: %v %v", selected.Unresolved, selected.ClosureErr)
	}
	return selected.Packs(), nil
}

func selectedPackRoot(entry PackEntry) (string, error) {
	if entry.PhysicalPath != "" {
		return entry.PhysicalPath, nil
	}
	if entry.LocalPath != "" {
		return entry.LocalPath, nil
	}
	address, err := url.Parse(entry.Source)
	if err != nil || address.Scheme != "file" || address.Path == "" {
		return "", fmt.Errorf("pack %s has no readable local source path", entry.Name)
	}
	return filepath.FromSlash(address.Path), nil
}

func selectedFetchedPackRoot(entry PackEntry) (string, error) {
	address, err := packsrc.Parse(entry.Source)
	if entry.AllowDefaultRef {
		address, err = packsrc.ParsePackAddress(entry.Source)
	}
	if err != nil {
		return "", fmt.Errorf("pack %s source address: %w", entry.Name, err)
	}
	store := &packsrc.Store{Dir: paths.PacksDir()}
	var resolved *packsrc.Resolved
	if entry.AllowDefaultRef && address.Ref == "" {
		resolved, err = store.ResolvePackDefault(address, entry.Name, entry.Slug(), entry.Source,
			packsrc.LockPath(paths.UserConfigPath()), true)
	} else {
		resolved, err = store.ResolveExisting(address, entry.Slug())
	}
	if err != nil {
		return "", fmt.Errorf("pack %s read-only source lookup: %w", entry.Name, err)
	}
	if resolved.StagedFrom != "" {
		return "", fmt.Errorf("pack %s only resolves through staged tree %s, whose source and filters cannot be proved for this planned entry; rerun the host launch or `yolo pack install` before editing", entry.Name, resolved.StagedFrom)
	}
	return resolved.Root, nil
}
