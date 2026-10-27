# Readable-config B2 activation boundary

B2's reader is a library seam, not runtime activation. `LoadWorkspaceConfig`,
`LoadWorkspaceConfigWithSources`, `UserScopeConfig`, `UserScopeConfigWithSources`,
`LoadConfig*`, `ResolveWorkspaceConfigPath`, and `WorkspaceConfigFileNames` deliberately retain
legacy filename, decoding, merge, and error behavior. They do not discover TOML. The original
`LoadWorkspaceConfig` TOML-acceptance starter remains deferred to H; it is not counted as passing.

Use the named library entry points for format-2 authored config:

- `LoadReadableWorkspaceConfig` / `LoadReadableWorkspaceConfigWithSources`
- `LoadReadableUserScopeConfig` / `LoadReadableUserScopeConfigWithSources`
- `LoadReadableConfig`, `LoadReadableConfigWithSources`, and
  `LoadReadableConfigWithoutWorkspaceFile`

They discover TOML before legacy filenames, refuse TOML/legacy sibling collisions, validate
format 2 and the shared schema/scope contract, read relative includes and sorted user `config.d`
fragments, apply schema-specific list clearing, anchor `env.files` at the declaring file, and
retain TOML/JSONC source positions. They are not runtime callers yet. The named-reader tests in
`readableconfig_test.go` are the permanent B2 library contract; the runtime-facade regression test
pins that H has not activated them prematurely.

## H activation checklist

After Root's main candidate is landed and the E/F/G semantic seams below are ready, H must switch
all runtime-facing read/composition facades and their consumers together, then rerun this census.
Production files that currently call the legacy public reader facades (from
`rg -l 'LoadWorkspaceConfig(WithSources)?\(|UserScopeConfig(WithSources)?\(|LoadConfig(WithSources|WithoutWorkspaceFile(WithSources)?)?\(' internal --glob '*.go' | grep -v _test.go`):

`internal/cli/apply.go`, `internal/cli/check/check.go`, `internal/cli/check/helpers.go`,
`internal/cli/commands.go`, `internal/cli/configdiff.go`, `internal/cli/configdrift.go`,
`internal/cli/configls.go`, `internal/cli/describe.go`, `internal/cli/internal.go`,
`internal/cli/preparationenv.go`, `internal/cli/programcapturedemand.go`,
`internal/cli/programcapturehost.go`, `internal/cli/run/inheritscope.go`,
`internal/cli/run/preflight.go`, `internal/config/adapters.go`, `internal/config/brokered.go`,
`internal/config/brokerfence.go`, `internal/config/drift.go`, `internal/config/gateread.go`,
`internal/config/hostfiles.go`, `internal/config/hostimportexclusions.go`,
`internal/config/hostmanagement.go`, `internal/config/hostwrappers.go`,
`internal/config/load.go`, `internal/config/localclones.go`, `internal/config/macosusermode.go`,
`internal/config/macosusersharedtmp.go`, `internal/config/mounts.go`, `internal/config/onrequest.go`,
`internal/config/packs.go`, `internal/config/profiles.go`, `internal/config/programs.go`,
`internal/config/provisioners.go`, `internal/config/relocations.go`,
`internal/config/sharehostfonts.go`, `internal/config/userlayer.go`, `internal/config/validate.go`,
`internal/config/validateproviders.go`, and `internal/loopholes/loopholescmd.go`.

Private-reader bypasses that must be migrated or intentionally remain legacy are in
`internal/config/adapters.go`, `hostfiles.go`, `hostmanagement.go`, `hostwrappers.go`, `mounts.go`,
`packs.go`, `profiles.go`, `programs.go`, `relocations.go`, and the facade implementations in
`load.go` and `userlayer.go` (`loadUserScopeConfig` / `loadUserScope`). `internal/entrypoint` and
`internal/cli/run` also own assembled-snapshot and runtime-record ingestion; this B2 change does not
edit those paths.

## Cross-owner activation dependencies

- **E — pack identity and selection:** `packs` and `pack_sources` are generically shape-checked,
  not resolved. H must consume E's canonical selected-pack/source result for host resolution and
  jail records. Do not reinterpret these fields as legacy `packages`, manifests, or pack handles.
- **F — provider/model/profile semantics:** provider declarations and `active_provider` are
  generically shape-checked, not resolved or composed. H must use F's host provider composition,
  credential/source rules, and model/profile selection result. No B2 provider selection or
  credential projection is provided.
- **G — allowance/request authority:** `allow` and `requests` are generically shape-checked, not
  translated into legacy `brokered` grants or activated. H must consume G's authority/request
  model and preserve its approval boundary. B2 intentionally makes no privilege conversion.

There is no whole-map compatibility projection: the new pack/provider/authority semantics do not
have a lossless legacy representation. The only compatibility guarantee in B2 is that the existing
legacy readers and their consumers remain unchanged until H's reviewed activation unit.
