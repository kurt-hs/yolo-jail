package packload

// modelreferences.go resolves a program's MODEL REFERENCES against its MODEL REGISTRY, both terms
// coined in packdecl.ModelReferences (docs/design/model-lists-and-pickers.md MM-D45): a value its
// config names a model by, or a pattern over models, and what the program can resolve one
// against. `yolo check` asks it of what the user and the packs DECLARE; the jail's boot asks it
// of the file the render WROTE.
//
// It is a pure evaluation, like EmptyModelLists: it reads no file, runs nothing and refuses
// nothing. A reference it cannot judge — its provider's ids include a catalog the caller could
// not read — is UNASKED, and a caller says so rather than calling it unknown, since "not found"
// and "could not ask" are different answers (MM-D16's rule).

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonptr"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
)

// ModelVerdict is one reference's answer.
type ModelVerdict int

const (
	// ModelKnown is a reference that resolves: a model the registry holds, or a pattern
	// matching at least one.
	ModelKnown ModelVerdict = iota
	// ModelUnknown is a reference that matches nothing in the registry.
	ModelUnknown
	// ModelUnasked is a reference the registry cannot judge: its provider's ids include a
	// catalog that could not be read.
	ModelUnasked
)

// ModelRegistry is one program's model registry: the ids it can resolve, by the program's own
// provider id.
type ModelRegistry struct {
	byProvider map[string]*registryProvider
}

type registryProvider struct {
	ids map[string]bool
	// unasked: some of this provider's ids are a catalog the caller could not read, or the
	// program runs it on rows yolo cannot see, so an id it does not hold is not known missing.
	unasked bool
}

// NewModelRegistry is agent's registry over the composed provider table: each listed id under
// the agent's own id for its provider (BuiltInProviderFor), and, for a provider the agent runs on
// a list of its own (RunsOwnList), every id of catalog, the ids agent's installed catalog names.
// A nil catalog is one that could not be read. An id a catalog names counts for every provider
// that reads it, the currency check's rule (MM-D16), since a catalog read here is not split by
// provider. A provider with no list that the agent does not run itself is unasked: the agent
// receives whatever its derive writes for it, which no table says.
func NewModelRegistry(packs []*Pack, providers *jsonx.OrderedMap, agent string, catalog map[string]bool) *ModelRegistry {
	r := &ModelRegistry{byProvider: map[string]*registryProvider{}}
	get := func(id string) *registryProvider {
		p := r.byProvider[id]
		if p == nil {
			p = &registryProvider{ids: map[string]bool{}}
			r.byProvider[id] = p
		}
		return p
	}
	own := BuiltInProvidersFor(packs, agent)
	withCatalog := func(p *registryProvider) {
		if catalog == nil {
			p.unasked = true
			return
		}
		for id := range catalog {
			p.ids[id] = true
		}
	}
	if providers != nil {
		for _, name := range providers.Keys() {
			id, ownList := name, false
			if b, ok := own[name]; ok {
				if b.ID == "" {
					continue
				}
				id, ownList = b.ID, !b.YoloList
			}
			p := get(id)
			rows := callableModelRows(providerEntry(providers, name))
			for _, row := range rows {
				p.ids[row.id] = true
			}
			switch {
			case ownList:
				withCatalog(p)
			case len(rows) == 0:
				p.unasked = true
			}
		}
	}
	for _, b := range own {
		if b.ID != "" && !b.YoloList && r.byProvider[b.ID] == nil {
			withCatalog(get(b.ID))
		}
	}
	return r
}

// Model is the verdict on a qualified `<provider>/<id>` reference, with the nearest ids the
// registry holds for an unknown one.
func (r *ModelRegistry) Model(qualified string) (ModelVerdict, []string) {
	provider, id, ok := strings.Cut(qualified, "/")
	p := r.byProvider[provider]
	if !ok || p == nil {
		return ModelUnknown, nearestStrings(qualified, r.qualified())
	}
	if p.ids[id] {
		return ModelKnown, nil
	}
	if p.unasked {
		return ModelUnasked, nil
	}
	return ModelUnknown, qualify(provider, nearestStrings(id, keys(p.ids)))
}

// Pattern is the verdict on a pattern over qualified ids (`*` matches any run of characters), with
// the nearest ids for an unknown one whose provider half is literal.
func (r *ModelRegistry) Pattern(pattern string) (ModelVerdict, []string) {
	re := globRegexp(pattern)
	provider, rest, _ := strings.Cut(pattern, "/")
	if !strings.Contains(provider, "*") {
		p := r.byProvider[provider]
		if p == nil {
			return ModelUnknown, nearestStrings(pattern, r.qualified())
		}
		for id := range p.ids {
			if re.MatchString(provider + "/" + id) {
				return ModelKnown, nil
			}
		}
		if p.unasked {
			return ModelUnasked, nil
		}
		return ModelUnknown, qualify(provider, nearestStrings(strings.ReplaceAll(rest, "*", ""), keys(p.ids)))
	}
	unasked := false
	for name, p := range r.byProvider {
		for id := range p.ids {
			if re.MatchString(name + "/" + id) {
				return ModelKnown, nil
			}
		}
		unasked = unasked || p.unasked
	}
	if unasked {
		return ModelUnasked, nil
	}
	return ModelUnknown, nil
}

// MatchesAny reports whether qualified matches one of patterns.
func MatchesAny(qualified string, patterns []string) bool {
	for _, p := range patterns {
		if globRegexp(p).MatchString(qualified) {
			return true
		}
	}
	return false
}

func (r *ModelRegistry) qualified() []string {
	var out []string
	for name, p := range r.byProvider {
		for id := range p.ids {
			out = append(out, name+"/"+id)
		}
	}
	return out
}

// globRegexp is pattern as an anchored regexp: `*` any run of characters, everything else itself.
func globRegexp(pattern string) *regexp.Regexp {
	parts := strings.Split(pattern, "*")
	for i, part := range parts {
		parts[i] = regexp.QuoteMeta(part)
	}
	return regexp.MustCompile("^" + strings.Join(parts, ".*") + "$")
}

// ModelReferenceValue is one model reference as declared or written: where it is, its value, and
// whether it is a pattern.
type ModelReferenceValue struct {
	// Where names the declaration in the reader's words: a config path such as
	// `config.profiles.or.subagents.default`, or a pack and pointer.
	Where string
	// Value is the reference as written.
	Value string
	// Qualified is the `<provider>/<id>` the program receives for it, when that differs from
	// Value (an alias resolved, a provider prefixed); "" otherwise.
	Qualified string
	Pattern   bool
}

// ModelReferenceFinding is one reference that matches nothing in the registry.
type ModelReferenceFinding struct {
	ModelReferenceValue
	// Nearest are up to three registry ids closest to it.
	Nearest []string
}

// Subject is what the registry was asked about: Qualified when set, else Value.
func (v ModelReferenceValue) Subject() string {
	if v.Qualified != "" {
		return v.Qualified
	}
	return v.Value
}

// EvaluateModelReferences is every value of values the registry does not resolve, in order, and
// how many it could not judge.
func EvaluateModelReferences(reg *ModelRegistry, values []ModelReferenceValue) ([]ModelReferenceFinding, int) {
	var out []ModelReferenceFinding
	unasked := 0
	for _, v := range values {
		verdict, nearest := reg.Model(v.Subject())
		if v.Pattern {
			verdict, nearest = reg.Pattern(v.Subject())
		}
		switch verdict {
		case ModelUnknown:
			out = append(out, ModelReferenceFinding{ModelReferenceValue: v, Nearest: nearest})
		case ModelUnasked:
			unasked++
		}
	}
	return out, unasked
}

// ModelReferenceValuesIn is every reference refs points at in doc, a decoded document (plain maps
// or jsonx.OrderedMap), each named by where followed by its concrete pointer.
func ModelReferenceValuesIn(doc any, refs *packdecl.ModelReferences, where string) []ModelReferenceValue {
	if refs == nil {
		return nil
	}
	var out []ModelReferenceValue
	for _, p := range refs.Models {
		for _, hit := range pointerHits(doc, p) {
			if s, ok := hit.value.(string); ok && s != "" {
				out = append(out, ModelReferenceValue{Where: where + hit.pointer, Value: s})
			}
		}
	}
	for _, p := range refs.Patterns {
		for _, hit := range pointerHits(doc, p) {
			items, _ := hit.value.([]any)
			for i, item := range items {
				if s, ok := item.(string); ok && s != "" {
					out = append(out, ModelReferenceValue{Where: fmt.Sprintf("%s%s/%d", where, hit.pointer, i),
						Value: s, Pattern: true})
				}
			}
		}
	}
	return out
}

type pointerHit struct {
	pointer string
	value   any
}

// pointerHits walks pointer through doc, a `*` token fanning out over every member, and returns
// each value reached with its concrete pointer, in sorted key order.
func pointerHits(doc any, pointer string) []pointerHit {
	tokens, err := jsonptr.Parse(pointer)
	if err != nil {
		return nil
	}
	var out []pointerHit
	var walk func(v any, rest, done []string)
	walk = func(v any, rest, done []string) {
		if len(rest) == 0 {
			out = append(out, pointerHit{pointer: jsonptr.Format(done), value: v})
			return
		}
		step := rest[0]
		next := func(key string, child any) {
			walk(child, rest[1:], append(append([]string(nil), done...), key))
		}
		switch t := v.(type) {
		case map[string]any:
			if step == "*" {
				for _, k := range sortedKeysOf(t) {
					next(k, t[k])
				}
			} else if child, ok := t[step]; ok {
				next(step, child)
			}
		case *jsonx.OrderedMap:
			if t == nil {
				return
			}
			if step == "*" {
				for _, k := range t.Keys() {
					child, _ := t.Get(k)
					next(k, child)
				}
			} else if child, ok := t.Get(step); ok {
				next(step, child)
			}
		case []any:
			if step == "*" {
				for i, child := range t {
					next(fmt.Sprint(i), child)
				}
			}
		}
	}
	walk(doc, tokens, nil)
	return out
}

// ProfileSubagentValues is the `subagents` declaration of agent's governing profile, as model
// references: each value resolved the way a derive resolves it (an alias of the provider's list
// to the id under it, else the value as an id) and qualified by agent's own id for the provider.
// Only the primary profile's declaration is read, the one a set's derive renders. Nil when the
// owning pack does not declare `profile_subagents`, or the profile declares nothing.
func ProfileSubagentValues(packs []*Pack, providers *jsonx.OrderedMap, profiles map[string]string,
	sets map[string][]string, resolved map[string]ResolvedProfile, agent string) []ModelReferenceValue {
	owner := binOwner(packs, agent)
	if owner == nil || owner.Decl == nil {
		return nil
	}
	refs := owner.Decl.ProgramModelReferences(agent)
	if refs == nil || !refs.ProfileSubagents {
		return nil
	}
	profile := profiles[agent]
	if set := sets[agent]; len(set) > 0 {
		profile = set[0]
	}
	r, ok := resolved[profile]
	if !ok || r.Provider == "" {
		return nil
	}
	provider := r.Provider
	if b, ok := BuiltInProviderFor(packs, agent, provider); ok {
		if b.ID == "" {
			return nil
		}
		provider = b.ID
	}
	models := childMap(providerEntry(providers, r.Provider), "models")
	value := func(where, v string) ModelReferenceValue {
		id := v
		if s, ok := stringAt(models, v); ok && s != "" {
			id = s
		}
		return ModelReferenceValue{Where: "config.profiles." + profile + ".subagents." + where, Value: v,
			Qualified: provider + "/" + id}
	}
	var out []ModelReferenceValue
	if d := r.Subagents.Default; d != "" {
		out = append(out, value("default", d))
	}
	for _, role := range sortedKeysOf(r.Subagents.Roles) {
		if v := r.Subagents.Roles[role]; v != "" {
			out = append(out, value("roles."+role, v))
		}
	}
	return out
}

// nearestStrings is up to three of candidates closest to s by edit distance, ties in sorted order.
func nearestStrings(s string, candidates []string) []string {
	type scored struct {
		c string
		d int
	}
	all := make([]scored, 0, len(candidates))
	for _, c := range candidates {
		all = append(all, scored{c, editDistance(s, c)})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].d != all[j].d {
			return all[i].d < all[j].d
		}
		return all[i].c < all[j].c
	})
	var out []string
	for i := 0; i < len(all) && i < 3; i++ {
		out = append(out, all[i].c)
	}
	return out
}

func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}

func qualify(provider string, ids []string) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = provider + "/" + id
	}
	return out
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func sortedKeysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
