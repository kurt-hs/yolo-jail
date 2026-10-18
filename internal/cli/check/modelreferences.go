package check

// modelreferences.go is MM-D45's check (docs/design/model-lists-and-pickers.md): every MODEL
// REFERENCE a selected program's pack declares (packdecl.ModelReferences, where the term is
// coined) — a profile's `subagents` models, and each model or pattern a pack writes at the
// declared pointers of the program's config surface — resolved against the program's MODEL
// REGISTRY, and a WARNING for one that matches nothing. Before it, such a value passed `yolo
// check`, landed in the agent's config verbatim, and failed only when a child agent was started
// on it, in another process, naming a model rather than the declaration that wrote it.
//
// Over the same prediction as the pairing gate (protocolPairingGap), so the providers and
// profiles are the ones the launch composes. The pack values are read from the selected packs'
// declarations through the collection the render folds (packoverlay.Collect), never a second
// reading of them. A reference whose provider runs on a catalog this check could not read is a
// SKIP, since "not found" and "could not ask" are different answers.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/agentcfg/manifest"
	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonptr"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/modelcatalog"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/packoverlay"
	"github.com/mschulkind-oss/yolo-jail/internal/render"
)

func (o *Options) modelReferenceReport(r *reporter, prediction protocolPrediction) {
	if !prediction.Ready {
		return
	}
	agents := map[string]bool{}
	for agent := range prediction.Profiles {
		agents[agent] = true
	}
	for agent := range prediction.Sets {
		agents[agent] = true
	}
	active := map[string][]string{}
	for agent := range agents {
		if set := prediction.Sets[agent]; len(set) > 0 {
			active[agent] = set
		} else if p := prediction.Profiles[agent]; p != "" {
			active[agent] = []string{p}
		}
	}
	autonomy := render.ProfileFor(render.KindJail).AgentAutonomy
	// The wire tables a launch exports, for the derives of each program's own model surfaces;
	// composed once, and only when a program declares one.
	var vars map[string]string
	wire := func() map[string]string {
		if vars == nil {
			use := jsonx.NewOrderedMap()
			for agent, set := range active {
				if len(set) > 1 {
					use.Set(agent, packload.ProfileSetWire(set))
				} else {
					use.Set(agent, set[0])
				}
			}
			vars = map[string]string{
				entrypoint.ProvidersWireEnv:   jsonx.MustDumpsCompact(prediction.Providers),
				entrypoint.ProfilesWireEnv:    jsonx.MustDumpsCompact(packload.ProfilesWireTable(prediction.Resolved)),
				entrypoint.UseProfilesWireEnv: jsonx.MustDumpsCompact(use),
			}
		}
		return vars
	}
	var overlays *packoverlay.OverlaySet
	catalogs := map[string]declaredCatalog{}
	for _, c := range o.declaredCatalogs(prediction.Packs) {
		catalogs[c.name] = c
	}
	for _, p := range prediction.Packs {
		if p == nil || p.Decl == nil {
			continue
		}
		for _, bin := range p.InstallBins() {
			refs := p.Decl.ProgramModelReferences(bin)
			if refs == nil {
				continue
			}
			if overlays == nil {
				overlays = packoverlay.Collect(prediction.Packs, autonomy, active)
			}
			sub := packload.ProfileSubagentValues(prediction.Packs, prediction.Providers, prediction.Profiles,
				prediction.Sets, prediction.Resolved, bin)
			for _, f := range sub.Findings {
				title, note := modelReferenceTitle(bin, f), modelReferenceNote(bin, f)
				if f.Refused {
					r.fail(title, note)
				} else {
					r.warn(title, note)
				}
			}
			values := append(sub.Values, packReferenceValues(prediction.Packs, p, refs, overlays, autonomy)...)
			if len(values) == 0 {
				if len(sub.Findings) == 0 && sub.Listed > 0 {
					r.ok(fmt.Sprintf("Model reference: every model %s's config names resolves in its registry (%s)",
						bin, plural(sub.Listed, "reference")+" checked"))
				}
				continue
			}
			var ids map[string]bool
			c, declared := catalogs[bin]
			if declared && c.dir != "" && modelcatalog.Installed(c.dir) {
				if cat := modelcatalog.Read(c.dir, c.globs); cat.Files > 0 {
					ids = cat.IDs
				}
			}
			reg := packload.NewModelRegistry(prediction.Packs, prediction.Providers, bin, ids)
			for _, src := range refs.Registry {
				if doc, err := entrypoint.DerivedSurface(p, src.Surface, wire(), prediction.Packs); err == nil {
					reg.AddRendered(doc, src)
				}
			}
			findings, unasked := packload.EvaluateModelReferences(reg, values)
			for _, f := range findings {
				r.warn(modelReferenceTitle(bin, f), modelReferenceNote(bin, f))
			}
			if unasked > 0 {
				why := "no catalog declared for it"
				if declared {
					why = c.missing
				}
				r.skip(fmt.Sprintf("Model reference: %s not checked against %s's registry — its provider "+
					"runs on %s's own catalog, which could not be read", plural(unasked, "model reference"), bin, bin), why)
			}
			if checked := len(values) - unasked + sub.Listed; len(findings) == 0 && len(sub.Findings) == 0 && checked > 0 {
				r.ok(fmt.Sprintf("Model reference: every model %s's config names resolves in its registry (%s)",
					bin, plural(checked, "reference")+" checked"))
			}
		}
	}
}

// modelReferenceTitle is a finding's headline: the declaration, the value, and what it matches.
func modelReferenceTitle(bin string, f packload.ModelReferenceFinding) string {
	what := "model"
	if f.Pattern {
		what = "pattern"
	}
	value := fmt.Sprintf("%q", f.Value)
	if f.Qualified != "" && f.Qualified != f.Value {
		value += fmt.Sprintf(" (%s receives %q)", bin, f.Qualified)
	}
	if f.Refused || f.Fallback != "" {
		return fmt.Sprintf("Model reference: %s names the %s %s, which is not on its provider's list",
			strings.TrimPrefix(f.Where, "config."), what, fmt.Sprintf("%q", f.Value))
	}
	return fmt.Sprintf("Model reference: %s names the %s %s, which matches no model in %s's registry",
		strings.TrimPrefix(f.Where, "config."), what, value, bin)
}

// modelReferenceNote is where the declaration was written, the nearest ids, and the next step.
func modelReferenceNote(bin string, f packload.ModelReferenceFinding) string {
	var parts []string
	if locs := config.UserScopeSources().Locations(f.Where); len(locs) > 0 {
		parts = append(parts, "Written at "+locs[0])
	}
	if len(f.Nearest) > 0 {
		parts = append(parts, "Nearest: "+strings.Join(f.Nearest, ", "))
	}
	switch {
	case f.Refused:
		parts = append(parts, fmt.Sprintf("It is neither an alias nor an id of the provider's list, which has no "+
			"`default` alias to fall back to, so %s's render refuses it and the launch stops", bin))
	case f.Fallback != "":
		parts = append(parts, fmt.Sprintf("It is neither an alias nor an id of the provider's list, so %s's child "+
			"agents start on the list's `default`, %s, instead", bin, f.Fallback))
	default:
		parts = append(parts, fmt.Sprintf("%s's registry is every id the composed provider table lists, the ids "+
			"%s's own model files hold, and %s's catalog for a provider it runs itself. A warning, never a "+
			"refusal: the launch starts, and a child agent started on it fails at dispatch", bin, bin, bin))
	}
	next := "Point it at one of the nearest ids"
	if strings.HasPrefix(f.Where, "config.") {
		next += " in your `" + strings.TrimPrefix(f.Where, "config.") + "`"
	} else {
		next += ", or ask that pack's author to"
	}
	return strings.Join(append(parts, next), ". ")
}

// packReferenceValues is every model reference the selected packs write at refs' pointers of the
// owner's surface: the owner's own `defaults` and `managed`, every config-overlay and every
// config-list entry folded onto it. Each is named by the pack and the pointer.
func packReferenceValues(packs []*packload.Pack, owner *packload.Pack, refs *packdecl.ModelReferences,
	overlays *packoverlay.OverlaySet, autonomy bool) []packload.ModelReferenceValue {
	key, err := manifest.ParseSurfaceID(refs.Surface)
	if err != nil {
		return nil
	}
	var out []packload.ModelReferenceValue
	surfaces, _ := owner.SurfacesFor(autonomy)
	for _, s := range surfaces {
		if s.Key() != key {
			continue
		}
		for layer, doc := range map[string]any{"defaults": s.Defaults, "managed": s.Managed} {
			out = append(out, packload.ModelReferenceValuesIn(doc, refs,
				fmt.Sprintf("pack %s (%s %s) ", owner.Name, refs.Surface, layer))...)
		}
	}
	for _, ov := range overlays.For(key.Agent, key.Name) {
		out = append(out, packload.ModelReferenceValuesIn(ov.Data, refs,
			fmt.Sprintf("pack %s (config-overlay on %s) ", ov.Pack, refs.Surface))...)
	}
	for _, l := range overlays.ListsFor(key.Agent, key.Name) {
		if !pointerDeclared(refs.Patterns, l.Path) {
			continue
		}
		for i, item := range l.Add {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, packload.ModelReferenceValue{Pattern: true, Value: s,
					Where: fmt.Sprintf("pack %s (config-list on %s) %s add[%d]", l.Pack, refs.Surface, l.Path, i)})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Where < out[j].Where })
	return out
}

// pointerDeclared reports whether concrete, a pointer with no wildcard, is one of declared, whose
// `*` tokens match any one token.
func pointerDeclared(declared []string, concrete string) bool {
	have, err := jsonptr.Parse(concrete)
	if err != nil {
		return false
	}
	for _, d := range declared {
		want, err := jsonptr.Parse(d)
		if err != nil || len(want) != len(have) {
			continue
		}
		match := true
		for i := range want {
			match = match && (want[i] == "*" || want[i] == have[i])
		}
		if match {
			return true
		}
	}
	return false
}
