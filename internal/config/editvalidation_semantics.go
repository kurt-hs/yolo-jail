package config

import (
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
)

func reportUnknownKeysWithReadableFG(config *jsonx.OrderedMap, errs *[]string) {
	allowed := make(map[string]struct{}, len(knownTopLevelConfigKeys)+3)
	for key := range knownTopLevelConfigKeys {
		allowed[key] = struct{}{}
	}
	for _, root := range []string{"active_provider", "allow", "requests"} {
		if _, recognized := readableSchemaRule([]string{root}); recognized {
			allowed[root] = struct{}{}
		}
	}
	reportUnknownKeys(config, allowed, "config", errs)
}

func validateProvidersForExplicit(config, readableEffective *jsonx.OrderedMap, ctx *editValidationContext, errs, warns *[]string) {
	validateProviderCredentialScopeConfig(ctx.rawWorkspace, errs)
	legacy := legacyProviderValidationProjection(config)
	validateProviderEntries(legacy, errs, warns)
	semantic, err := BuildReadableSemanticInputs(ReadableSemanticInputSource{
		Config: readableEffective, User: ctx.rawUser, Workspace: ctx.rawWorkspace,
		UserOrigins:      readableUserOrigins(ctx.rawUser, ctx.userOrigins),
		WorkspaceOrigins: readableWorkspaceOrigins(ctx.rawWorkspace, ctx.workspaceOrigins),
		Packs:            ctx.packs, Catalog: ctx.catalog,
	})
	if err != nil {
		add(errs, err.Error())
		return
	}
	ctx.semantic = &semantic
}

func legacyProviderValidationProjection(config *jsonx.OrderedMap) *jsonx.OrderedMap {
	projected := cloneValidationMap(config)
	if projected == nil {
		return projected
	}
	value, present := projected.Get(providersKey)
	providers, ok := asMap(value)
	if !present || !ok {
		return projected
	}
	decoded, err := packdecl.DecodeProviderTable(config)
	if err != nil {
		return projected
	}
	legacyModels := map[string]map[string]bool{}
	for _, declaration := range decoded.Declarations {
		if len(declaration.Models) == 0 {
			continue
		}
		legacyModels[declaration.Name] = map[string]bool{}
		for id := range declaration.Models {
			legacyModels[declaration.Name][id] = true
		}
	}
	for _, name := range decoded.Disabled {
		providers.Delete(name)
	}
	canonicalOnly := map[string]bool{}
	for _, key := range ReadableSchemaKeys() {
		parts := strings.Split(key.Path, ".")
		if len(parts) < 3 || parts[0] != providersKey || parts[1] != "*" {
			continue
		}
		if _, legacyKey := knownProviderKeys[parts[2]]; !legacyKey {
			canonicalOnly[parts[2]] = true
		}
	}
	for _, name := range providers.Keys() {
		raw, _ := providers.Get(name)
		entry, ok := asMap(raw)
		if !ok {
			continue
		}
		for key := range canonicalOnly {
			entry.Delete(key)
		}
		if modelsValue, present := entry.Get("models"); present {
			models, ok := asMap(modelsValue)
			if ok {
				for _, id := range models.Keys() {
					if !legacyModels[name][id] {
						models.Delete(id)
					}
				}
			}
		}
		providers.Set(name, entry)
	}
	projected.Set(providersKey, providers)
	return projected
}
