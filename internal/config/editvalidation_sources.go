package config

import (
	"fmt"
	"io/fs"
	"net/url"
	"path/filepath"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func (ctx *editValidationContext) selectedPacks() ([]*packload.Pack, error) {
	if ctx.rawUser == nil {
		return nil, fmt.Errorf("planned user config source is missing from explicit edit context")
	}
	if ctx.embeddedNamesErr != nil {
		return nil, fmt.Errorf("enumerate the captured embedded pack source: %w; rerun with a complete yolo build", ctx.embeddedNamesErr)
	}
	entries, problems := lowerReadablePackEntriesFromEmbeddedNames(ctx.rawUser, ctx.userOrigins, ctx.embeddedNames)
	if len(problems) != 0 {
		return nil, fmt.Errorf("read planned pack entries: %v", problems)
	}
	entries = appendImplicitLocalPack(entries)
	fsys := ctx.embeddedFS
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
			if _, active := ctx.rawUser.Get("active_provider"); !active {
				return ConfigProfileTable(ctx.rawUser, set)
			}
			var closureProfiles map[string]packload.UserProfile
			var closureErr error
			active, closureProfiles, closureErr := explicitEditViaSelection(ctx.rawUser, set)
			ctx.viaSelectionErr = closureErr
			ctx.viaProfiles = closureProfiles
			return active
		},
		UserProfiles: func() (map[string]packload.UserProfile, error) {
			if ctx.viaSelectionErr != nil {
				return nil, ctx.viaSelectionErr
			}
			if _, active := ctx.rawUser.Get("active_provider"); active {
				return clonePackUserProfiles(ctx.viaProfiles), nil
			}
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
	if ctx.viaSelectionErr != nil {
		return nil, fmt.Errorf("resolve planned active provider selection for pack closure: %w", ctx.viaSelectionErr)
	}
	if embeddedErr != nil {
		return nil, fmt.Errorf("planned pack closure source: %w", embeddedErr)
	}
	if !selected.Complete() {
		return nil, fmt.Errorf("planned pack selection is incomplete: %v %v", selected.Unresolved, selected.ClosureErr)
	}
	return selected.Packs(), nil
}

func explicitEditViaSelection(config *jsonx.OrderedMap, packs []*packload.Pack) (map[string]string, map[string]packload.UserProfile, error) {
	var authored *jsonx.OrderedMap
	if raw, present := config.Get(providersKey); present {
		var ok bool
		authored, ok = raw.(*jsonx.OrderedMap)
		if !ok {
			return nil, nil, fmt.Errorf("config.providers: expected a table")
		}
	}
	declarations, err := packload.ComposeProviderDeclarations(authored, packs)
	if err != nil {
		return nil, nil, fmt.Errorf("compose selected provider declarations: %w", err)
	}
	selection, err := ConfigActiveProviderSelection(config)
	if err != nil {
		return nil, nil, err
	}
	resolved, err := ResolveActiveProviders(declarations, selection, ProviderReceivers(packs))
	if err != nil {
		return nil, nil, err
	}
	active := make(map[string]string)
	profiles := make(map[string]packload.UserProfile)
	for receiver, targets := range resolved.ByReceiver {
		if len(targets) == 0 || targets[0].Target.Native {
			continue
		}
		// ResolveVias consumes the existing singular profile table. The F selector result
		// is already in precedence order; use its primary provider just as the legacy table
		// exposes its primary profile, without guessing from authored aliases.
		target := targets[0]
		profile := "\x00readable-provider:" + target.Provider
		if target.Target.Variant != "" {
			profile += "/" + target.Target.Variant
		}
		active[receiver] = profile
		profiles[profile] = packload.UserProfile{Provider: target.Provider, Via: target.Via}
	}
	return active, profiles, nil
}

func clonePackUserProfiles(source map[string]packload.UserProfile) map[string]packload.UserProfile {
	cloned := make(map[string]packload.UserProfile, len(source))
	for name, profile := range source {
		cloned[name] = profile
	}
	return cloned
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
