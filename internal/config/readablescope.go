package config

import (
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/brokerscope"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

type ReadableAuthorityError struct {
	Cause error
}

func (e *ReadableAuthorityError) Error() string {
	return "readable broker authority could not be resolved: " + e.Cause.Error()
}

func (e *ReadableAuthorityError) Unwrap() error { return e.Cause }

// readableScopeCheck adapts the explicit readable allowance contract at the existing public
// scope producer. Callers opt in by passing the selected source declaration; legacy callers keep
// their current read path until their runtime handoff supplies that declaration.
func readableScopeCheck(workspace string, brokers []ScopeBroker, read *WorkspaceRead) (*ScopeCheck, bool) {
	if read == nil || read.Config == nil {
		return nil, false
	}
	for _, broker := range brokers {
		if broker.Readable == nil || broker.Readable.Brokered.Source != broker.Source {
			return nil, false
		}
	}
	user, userSources, err := LoadReadableUserScopeConfigWithSources(true, func(string) {})
	if err != nil {
		return readableScopeFailure(workspace, brokers, err), true
	}
	_, userHasAllow := user.Get("allow")
	_, workspaceHasRequests := read.Config.Get("requests")
	if !userHasAllow && !workspaceHasRequests {
		return nil, false
	}
	declarations := make([]ReadableBrokerDeclaration, 0, len(brokers))
	selected := make([]string, 0, len(brokers))
	for _, broker := range brokers {
		declaration := *broker.Readable
		declarations = append(declarations, declaration)
		selected = append(selected, declaration.Pack)
	}
	userOrigins := readableUserOrigins(user, userSources)
	if !userHasAllow {
		legacyOrigins := applyLegacyReadableUserAllowance(user, declarations)
		for key, files := range legacyOrigins {
			userOrigins[key] = files
		}
	}
	workspaceOrigins := readableWorkspaceOrigins(read.Config, read.sources)
	plan, err := ResolveReadableAllowance(ReadableAllowanceInput{
		User: user, Workspace: read.Config, SelectedPacks: selected, Declarations: declarations,
		UserOrigins: userOrigins, WorkspaceOrigins: workspaceOrigins,
	})
	if err != nil {
		return readableScopeFailure(workspace, brokers, err), true
	}
	collectLegacyReadableRepositories(&plan, read, brokers)
	gate, err := ProduceReadableGateInput(workspace, plan)
	if err != nil {
		return readableScopeFailure(workspace, brokers, err), true
	}
	applyLegacyReadableWorkspace(read, user, brokers, &gate)
	return scopeCheckFromReadable(workspace, brokers, gate), true
}

func readableScopeFailure(workspace string, brokers []ScopeBroker, cause error) *ScopeCheck {
	rec, _, _ := readScopeRecord(ApprovalScopePath(workspace))
	check := &ScopeCheck{Fatal: &ReadableAuthorityError{Cause: cause}}
	for _, broker := range brokers {
		source := ScopeSource{Source: broker.Source, Label: broker.Label, RemoteHost: broker.RemoteHost,
			Form: brokerscope.FormFor(broker.RemoteHost), Read: brokerscope.Read{Problem: "readable broker authority could not be resolved: " + cause.Error()}}
		source.Dormant = !recordHeldRead(rec, broker.Source)
		check.Sources = append(check.Sources, source)
	}
	return check
}

func hasMatchingRemoteTarget(targets []ReadableRepository, requested ReadableRepository) bool {
	for _, target := range targets {
		if strings.HasPrefix(target.Origin, "remote ") && target.RemoteHost == requested.RemoteHost && strings.EqualFold(target.Repository, requested.Repository) {
			return true
		}
	}
	return false
}

func collectLegacyReadableRepositories(plan *ReadableAllowancePlan, read *WorkspaceRead, brokers []ScopeBroker) {
	if plan == nil || read == nil {
		return
	}
	for _, broker := range brokers {
		if broker.RemoteHost == brokerscope.AnyRemoteHost {
			continue
		}
		entries := read.Entry(broker.Source)
		if len(entries) == 0 {
			continue
		}
		index := readableBrokerIndex(plan.Sources, broker.Source)
		if index < 0 {
			continue
		}
		policy := &plan.Sources[index]
		for _, entry := range entries {
			policy.Repos = appendReadableRepo(policy.Repos, ReadableRepoRequest{Repository: entry.Repo,
				Path: brokeredReposPath(broker.Source), Files: cloneOrigins(entry.Files)})
		}
	}
}

func applyLegacyReadableWorkspace(read *WorkspaceRead, user *jsonx.OrderedMap, brokers []ScopeBroker, gate *ReadableGateInput) {
	if read == nil || gate == nil {
		return
	}
	for _, broker := range brokers {
		if broker.Readable == nil {
			continue
		}
		entries := read.Entry(broker.Source)
		index := -1
		for i := range gate.Sources {
			if gate.Sources[i].Source == broker.Source {
				index = i
				break
			}
		}
		readAllowed := readableUserAllows(user, broker.Source, "always", broker.Readable.ReadCapability)
		if index < 0 {
			if !readAllowed {
				continue
			}
			var observed ReadableObservedSource
			for _, candidate := range gate.ObservedSources {
				if candidate.Source == broker.Source {
					observed = candidate
					break
				}
			}
			gate.Sources = append(gate.Sources, ReadableGateSource{Source: broker.Source, Label: broker.Label,
				RemoteHost: broker.RemoteHost, Read: cloneBrokerScopeRead(observed.Read)})
			index = len(gate.Sources) - 1
		}
		current := &gate.Sources[index]
		for _, entry := range entries {
			current.Entry = appendReadableEntry(current.Entry, entry)
			target := ReadableRepository{Source: broker.Source, RemoteHost: broker.RemoteHost, Repository: entry.Repo,
				Origin: "brokered." + broker.Source + ".repos", Files: cloneOrigins(entry.Files)}
			current.Targets = appendReadableRepository(current.Targets, target)
			if readAllowed {
				grant := brokerscope.Grant{Kind: brokerscope.KindRead, Repo: target.RemoteHost + "/" + target.Repository}
				current.Grants = appendReadableConcreteGrant(current.Grants, grant)
				current.Decisions = append(current.Decisions, ReadableGrantDecision{Grant: grant, Origins: []string{"allow." + broker.Source + ".always"}, Files: cloneOrigins(entry.Files)})
			}
		}
		legacy, files := rawWorkspaceReadableGrants(read, broker.Source, broker.Readable.Brokered.Grants)
		for _, grant := range legacy {
			capability := broker.Readable.KindCapabilities[grant.Kind]
			if capability == "" || !readableUserAllowsEither(user, broker.Source, capability) {
				continue
			}
			if grant.Repo == brokerscope.AnyRepo {
				if brokerscope.GrantTakesBranch(grant.Kind) {
					gate.Differences = appendReadableDifference(gate.Differences, "legacy "+broker.Source+" branch grant cannot be projected across repository scope without widening; preserve the original brokered grant and write no readable request")
					continue
				}
				gate.Differences = appendReadableDifference(gate.Differences, "legacy "+broker.Source+" wildcard repository grant cannot be projected to finite remotes without changing future scope; preserve it as a migration difference and write nothing")
				continue
			}
			matched := false
			for _, entry := range entries {
				if strings.EqualFold(entry.Repo, grant.Repo) || strings.EqualFold(broker.RemoteHost+"/"+entry.Repo, grant.Repo) {
					grant.Repo = broker.RemoteHost + "/" + entry.Repo
					matched = true
					break
				}
			}
			if !matched {
				for _, target := range current.Targets {
					concreteRepo := target.RemoteHost + "/" + target.Repository
					if strings.EqualFold(concreteRepo, grant.Repo) ||
						(broker.RemoteHost != brokerscope.AnyRemoteHost && strings.EqualFold(target.Repository, grant.Repo)) {
						grant.Repo = concreteRepo
						matched = true
						break
					}
				}
			}
			if !matched {
				gate.Differences = appendReadableDifference(gate.Differences, "legacy "+broker.Source+" grant "+grant.String()+" is outside its concrete remote targets; preserve as a migration difference and write nothing")
				continue
			}
			current.Grants = appendReadableConcreteGrant(current.Grants, grant)
			current.Decisions = append(current.Decisions, ReadableGrantDecision{Grant: grant, Origins: []string{"brokered." + broker.Source + ".grants"}, Files: cloneOrigins(files)})
		}
	}
}

func rawWorkspaceReadableGrants(read *WorkspaceRead, source string, defined []string) ([]brokerscope.Grant, []string) {
	if read == nil || read.Config == nil {
		return nil, nil
	}
	value, _ := read.Config.Get(brokeredKey)
	sources, ok := asMap(value)
	if !ok {
		return nil, nil
	}
	sourceValue, _ := sources.Get(source)
	table, ok := asMap(sourceValue)
	if !ok {
		return nil, nil
	}
	value, _ = table.Get(brokeredGrantsKey)
	list, _ := value.([]any)
	node := read.sources.node().key(brokeredKey).key(source).key(brokeredGrantsKey)
	var grants []brokerscope.Grant
	var files []string
	for i, value := range list {
		m, ok := asMap(value)
		if !ok {
			continue
		}
		kindValue, _ := m.Get(brokeredGrantKindKey)
		repoValue, _ := m.Get(brokeredGrantRepoKey)
		branchValue, hasBranch := m.Get(brokeredGrantBranchKey)
		kind, _ := kindValue.(string)
		repo, _ := repoValue.(string)
		branch, branchOK := branchValue.(string)
		wantKeys := 2
		if hasBranch {
			wantKeys = 3
		}
		grant := brokerscope.Grant{Kind: kind, Repo: repo, Branch: branch}
		if len(m.Keys()) != wantKeys || (hasBranch && !branchOK) || !brokerscope.ValidGrant(grant) || !inList(defined, kind) {
			continue
		}
		grants = appendReadableConcreteGrant(grants, grant)
		for _, writer := range node.elem(i).writerList() {
			if writer.file != nil && writer.file.contained && writer.file.rel != "" {
				files = appendUniqueStrings(files, writer.file.rel)
			}
		}
	}
	return grants, files
}

func readableUserAllows(user *jsonx.OrderedMap, source, key, capability string) bool {
	if capability == "" {
		return false
	}
	allowValue, _ := user.Get("allow")
	allow, ok := asMap(allowValue)
	if !ok {
		return false
	}
	sourceValue, _ := allow.Get(source)
	table, ok := asMap(sourceValue)
	if !ok {
		return false
	}
	value, _ := table.Get(key)
	values, err := readableStringList(value, "allow."+source+"."+key)
	if err != nil {
		return false
	}
	return inList(values, capability)
}

func readableUserAllowsEither(user *jsonx.OrderedMap, source, capability string) bool {
	return readableUserAllows(user, source, "always", capability) || readableUserAllows(user, source, "on_request", capability)
}

func applyLegacyReadableUserAllowance(user *jsonx.OrderedMap, declarations []ReadableBrokerDeclaration) map[string][]string {
	legacyPath, _, readable, err := resolveUserConfigPath(paths.UserConfigPath())
	if err != nil || readable || legacyPath == "" {
		return nil
	}
	var allow *jsonx.OrderedMap
	origins := map[string][]string{}
	sources := UserScopeSources()
	for _, declaration := range declarations {
		source := declaration.Brokered.Source
		table := jsonx.NewOrderedMap()
		if containsGrant(BrokeredUserGrants(source), brokerscope.Grant{Kind: brokerscope.KindRead, Repo: brokerscope.AnyRepo}) && declaration.ReadCapability != "" {
			table.Set("always", []any{declaration.ReadCapability})
			origins["allow."+source+".always"] = scopeSourceFiles(sources, "brokered", source, brokeredGrantsKey)
		}
		allowedKinds := BrokeredGrantKinds(source)
		var onRequest []any
		for _, kind := range declaration.Brokered.Grants {
			if !inList(allowedKinds, kind) {
				continue
			}
			if capability := declaration.KindCapabilities[kind]; capability != "" && !containsStringAny(onRequest, capability) {
				onRequest = append(onRequest, capability)
			}
		}
		if len(onRequest) > 0 {
			table.Set("on_request", onRequest)
			origins["allow."+source+".on_request"] = scopeSourceFiles(sources, "brokered", source, brokeredGrantKindsKey)
		}
		if len(table.Keys()) == 0 {
			continue
		}
		if allow == nil {
			allow = jsonx.NewOrderedMap()
		}
		allow.Set(source, table)
	}
	if allow != nil {
		user.Set("allow", allow)
	}
	return origins
}

func containsStringAny(values []any, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func readableWorkspaceOrigins(config *jsonx.OrderedMap, sources *Sources) map[string][]string {
	origins := map[string][]string{}
	var raw any
	if config != nil {
		raw, _ = config.Get("requests")
	}
	requests, ok := asMap(raw)
	if !ok {
		return origins
	}
	for _, source := range requests.Keys() {
		values, ok := asMap(readableValue(requests, source))
		if !ok {
			continue
		}
		origins["requests."+source] = scopeSourceFiles(sources, "requests", source)
		for _, key := range values.Keys() {
			path := []string{"requests", source, key}
			origins[strings.Join(path, ".")] = scopeSourceFiles(sources, path...)
		}
	}
	return origins
}

func readableUserOrigins(config *jsonx.OrderedMap, sources *Sources) map[string][]string {
	origins := map[string][]string{}
	var raw any
	if config != nil {
		raw, _ = config.Get("allow")
	}
	allow, ok := asMap(raw)
	if !ok {
		return origins
	}
	for _, source := range allow.Keys() {
		values, ok := asMap(readableValue(allow, source))
		if !ok {
			continue
		}
		origins["allow."+source] = scopeSourceFiles(sources, "allow", source)
		for _, key := range values.Keys() {
			path := []string{"allow", source, key}
			origins[strings.Join(path, ".")] = scopeSourceFiles(sources, path...)
		}
	}
	return origins
}

func scopeSourceFiles(sources *Sources, parts ...string) []string {
	if sources == nil || sources.root == nil {
		return nil
	}
	node := sources.root
	for _, part := range parts {
		node = node.key(part)
		if node == nil {
			return nil
		}
	}
	var files []string
	for _, writer := range node.writerList() {
		if writer.file == nil {
			continue
		}
		name := writer.file.rel
		if name == "" {
			name = writer.file.path
		}
		if !inList(files, name) {
			files = append(files, name)
		}
	}
	return files
}

func scopeCheckFromReadable(workspace string, brokers []ScopeBroker, input ReadableGateInput) *ScopeCheck {
	rec, _, _ := readScopeRecord(ApprovalScopePath(workspace))
	observed := make(map[string]ReadableObservedSource, len(input.ObservedSources))
	for _, source := range input.ObservedSources {
		observed[source.Source] = source
	}
	concrete := make(map[string]ReadableGateSource, len(input.Sources))
	for _, source := range input.Sources {
		concrete[source.Source] = source
	}
	check := &ScopeCheck{ReadableInput: &input}
	for _, broker := range brokers {
		source := ScopeSource{Source: broker.Source, Label: broker.Label, RemoteHost: broker.RemoteHost,
			Form: brokerscope.FormFor(broker.RemoteHost)}
		if current, ok := observed[broker.Source]; ok {
			source.Read = current.Read
			if current.EntryKey != "" {
				source.EntryKey = current.EntryKey
			}
			for _, target := range current.Targets {
				if strings.HasPrefix(target.Origin, "requests.") && strings.HasSuffix(target.Origin, ".repos") {
					source.EntryKey = target.Origin
				}
				if strings.HasPrefix(target.Origin, "requests.") && broker.RemoteHost != brokerscope.AnyRemoteHost && target.RemoteHost == broker.RemoteHost {
					source.Entry = appendReadableEntry(source.Entry, EntryRepo{Repo: target.Repository, Files: cloneOrigins(target.Files)})
				}
			}
		}
		if current, ok := concrete[broker.Source]; ok {
			source.Read = current.Read
			if current.EntryKey != "" {
				source.EntryKey = current.EntryKey
			}
			for _, target := range current.Targets {
				if strings.HasPrefix(target.Origin, "brokered.") && source.EntryKey == "" {
					source.EntryKey = brokeredReposPath(broker.Source)
				}
			}
			for _, entry := range current.Entry {
				source.Entry = appendReadableEntry(source.Entry, entry)
			}
			var unrepresentable []ReadableRepository
			for _, target := range current.Targets {
				if broker.RemoteHost == brokerscope.AnyRemoteHost && strings.HasPrefix(target.Origin, "requests.") && !hasMatchingRemoteTarget(current.Targets, target) {
					unrepresentable = append(unrepresentable, target)
				}
			}
			if len(unrepresentable) > 0 {
				for _, target := range unrepresentable {
					note := "readable target " + target.RemoteHost + "/" + target.Repository + " cannot be encoded by this remote-URL scope; add an exact remote with `git remote add` and rerun; no grant was issued"
					if source.Read.Problem == "" {
						source.Read.Problem = note
					} else {
						source.Read.Problem += "; " + note
					}
				}
			}
			source.Grants = append([]brokerscope.Grant(nil), current.Grants...)
			if len(unrepresentable) > 0 {
				filtered := source.Grants[:0]
				for _, grant := range source.Grants {
					matches := false
					for _, target := range unrepresentable {
						matches = matches || strings.EqualFold(grant.Repo, target.RemoteHost+"/"+target.Repository)
					}
					if !matches {
						filtered = append(filtered, grant)
					}
				}
				source.Grants = filtered
			}
			for _, decision := range current.Decisions {
				unrepresentableDecision := false
				for _, target := range unrepresentable {
					unrepresentableDecision = unrepresentableDecision || strings.EqualFold(decision.Grant.Repo, target.RemoteHost+"/"+target.Repository)
				}
				if unrepresentableDecision {
					continue
				}
				for _, origin := range decision.Origins {
					if strings.HasPrefix(origin, "requests.") {
						if source.GrantPaths == nil {
							source.GrantPaths = map[string][]string{}
						}
						for _, request := range input.Requests {
							if request.Path == origin && request.Status == ReadableRequestGranted {
								source.GrantPaths[origin] = appendUniqueStrings(source.GrantPaths[origin], request.Files...)
							}
						}
						for _, target := range current.Targets {
							if target.Origin == origin {
								source.GrantPaths[origin] = appendUniqueStrings(source.GrantPaths[origin], target.Files...)
							}
						}
					}
					if strings.HasPrefix(origin, "brokered.") {
						source.GrantFiles = appendUniqueStrings(source.GrantFiles, decision.Files...)
					}
					if strings.HasPrefix(origin, "allow.") && !containsGrant(source.UserGrants, decision.Grant) {
						source.UserGrants = append(source.UserGrants, decision.Grant)
					}
				}
			}
		}
		if len(input.Differences) > 0 {
			source.Read.Problem = strings.Join(input.Differences, "; ")
		}
		if !brokerscope.HoldsRead(source.Grants) {
			source.Read.Remotes = nil
			source.Entry = nil
			source.Grants = nil
			source.GrantFiles = nil
			source.UserGrants = nil
			source.Dormant = !recordHeldRead(rec, broker.Source)
		} else {
			source.FirstRead = !recordHeldRead(rec, broker.Source)
		}
		check.Sources = append(check.Sources, source)
	}
	return check
}

func appendUniqueStrings(values []string, additions ...string) []string {
	for _, addition := range additions {
		if addition != "" && !inList(values, addition) {
			values = append(values, addition)
		}
	}
	return values
}

func containsGrant(grants []brokerscope.Grant, want brokerscope.Grant) bool {
	for _, grant := range grants {
		if grant == want {
			return true
		}
	}
	return false
}
