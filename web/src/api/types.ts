// Types mirroring api/v1/openapi-template-builder.yaml. Hand-written and kept
// wire-compatible with the spec (which the Go server types are generated from).
//
// Follow-up: these could be generated with openapi-typescript. Not adopted yet
// because its output uses a nested `components['schemas'][...]` shape that isn't
// a drop-in for the named interfaces this module exports, so switching would
// mean reshaping every consumer. The `kernel?` fields below are forward-looking
// UI state the backend currently ignores (not in the spec).

export interface Option {
  id: string
  displayName: string
}

export interface Target {
  id: string
  displayName: string
  os: string
  arch: string
}

export interface Combination {
  vertical: string
  sku?: string
  platform: string
  os: string
  // Optional kernel variant (e.g. "standard" | "rt"). Present only when a
  // vertical/platform/OS offers a real-time template variant; the UI gates the
  // kernel selector on its presence rather than hardcoding RT support.
  kernel?: string
  imageType: string
  template: string
}

export interface Manifest {
  combinations: Combination[]
  verticals: Option[]
  skus: Option[]
  platforms: Option[]
  targets: Target[]
}

export interface ComposeRequest {
  vertical: string
  sku?: string
  platform: string
  os: string
  kernel?: string
  imageType: string
  // Advanced-mode override for the matched template's image.name. Omitted (not
  // just empty) means "not overridden" — the backend resolves the curated
  // template directly rather than generating an override delta for it.
  imageName?: string
  // Packages picked in the Packages step, already encoded (see encodePackage in
  // store.ts): a bare name floats to whatever the repo publishes, `name_version`
  // pins that exact version. Emitted into the delta's systemConfig.packages.
  packages?: string[]
  // Repository ids the user enabled. Each becomes a packageRepositories entry in
  // the delta, so a package from a repo the curated template doesn't configure
  // can still resolve. Base repos (enabledByDefault) need not be sent.
  repos?: string[]
  // The Disk step's edited disk block, emitted into the delta's `disk`.
  // Omitted (not just empty) means "not overridden".
  //
  // Must be the COMPLETE block, not a diff: the extends merge replaces `disk`
  // wholesale (internal/config/merge.go) rather than merging field by field the
  // way it unions package lists, so a partial block would drop whatever it
  // omits — the parent's whole partition table included. toDiskConfig() emits a
  // complete block because the model is seeded from the resolved template.
  disk?: Record<string, unknown>
}

export interface ComposeSummary {
  // Selection echo
  vertical: string
  sku: string
  platform: string
  os: string
  imageType: string
  // Template-derived
  imageName: string
  imageVersion: string
  description: string
  architecture: string
  kernelVersion: string
  packageCount: number
  diskSize: string
  partitionCount: number
  partitionTable: string
  hostname: string
  baseImage?: string
}

export interface ComposeResponse {
  template: string
  yaml: string
  summary: ComposeSummary
  // The generated extends delta, verbatim — only what this request overrode.
  // Absent when nothing was overridden, since no delta is generated then.
  deltaYaml?: string
  // The curated template resolved *without* this request's overrides, so the
  // baseline can be shown beside the delta that modifies it. Absent when there
  // are no overrides, because `yaml` already is the base.
  baseYaml?: string
  // Packages requested at a pinned version whose name the curated template
  // already lists unpinned. The extends merge unions package lists and cannot
  // drop the parent's entry, so both survive into `yaml`. Advisory only.
  pinConflicts?: string[]
  // The matched template's own package list, before this request's overrides
  // are applied. Present even when the request carries no overrides, so the
  // Packages step can show "already included" packages alongside whatever
  // the user is adding.
  basePackages?: string[]
}

// One issue from POST /templates/validate: a schema/semantic problem tied to a
// field path. severity distinguishes a blocking error from an advisory warning.
export interface ValidationIssue {
  path: string
  message: string
  severity: 'error' | 'warning'
}

// Result of validating an edited template. A failed validation is still a
// successful 200 call — `valid` reports the outcome; `errors`/`warnings` carry
// the per-field issues. Backs PR 2 onward; the endpoint returns 501 until then.
export interface ValidationResponse {
  valid: boolean
  errors?: ValidationIssue[]
  warnings?: ValidationIssue[]
}

// One repository the Advanced tab can enable/disable (from GET /package-repos).
// enabledByDefault seeds the toggle; priority breaks ties when a package exists
// in multiple repos (higher wins).
export interface PackageRepo {
  id: string
  displayName: string
  url: string
  description?: string
  enabledByDefault: boolean
  priority?: number
  // Whether the repo defines any "frequently used" packages, i.e. whether a
  // curated search can return anything for it. The list itself stays
  // server-side; this only says whether the toggle is worth offering.
  hasCuratedPackages?: boolean
  // Whether the catalog knows a GPG signing-key URL for this repo. When false,
  // enabling it emits a packageRepositories entry with no key, so a build
  // fetches its packages without verifying the repo's signature. The key URL
  // itself stays server-side; this only says whether to warn.
  hasSigningKey?: boolean
}

export interface PackageRepoList {
  repos: PackageRepo[]
}

// One package search hit (from GET /packages/search): name + latest version +
// description, plus the repository it came from.
// One available version of a package and the repository providing it. Pinning
// a version therefore also picks a repository.
export interface PackageVersion {
  version: string
  repository: string
}

export interface PackageSearchResult {
  name: string
  version: string
  description?: string
  repository: string
  // Every version found, newest first (server-ordered by the target's own
  // version rules, not string order). `version`/`repository` mirror
  // `versions[0]`. Optional because a repository can be searched by an older
  // backend that predates the field.
  versions?: PackageVersion[]
}

export interface PackageSearchResults {
  query: string
  total: number
  packages: PackageSearchResult[]
}

// The Edge Pack grouping for one target (from GET /edge-pack): the same
// packages the repository browser offers, arranged by what they let an image do
// rather than by where they come from. Both surfaces write into one selection.
export interface EdgePack {
  id: string
  displayName: string
  description?: string
  // The PackageRepo id every package in the pack resolves from. Selecting a
  // package must enable this repo, exactly as picking a search hit enables the
  // repo it came from — otherwise the package cannot resolve at build time.
  repo: string
  // False when `repo` isn't offered for this target at all, i.e. the pack can't
  // be used here. The UI says so rather than rendering an empty domain grid.
  repoAvailable: boolean
  // The runtime flavours a domain's packages sit on top of. Common to every
  // domain rather than owned by one, which is why they're listed here and not
  // per-domain. No domain is selectable until one is chosen.
  baseRuntimes: EdgePackBaseRuntime[]
  domains: EdgePackDomain[]
}

export interface EdgePackBaseRuntime {
  id: string
  displayName: string
  package: EdgePackPackage
  // False means shown but unselectable; unavailableReason always says why.
  available: boolean
  unavailableReason?: string
}

export interface EdgePackDomain {
  id: string
  displayName: string
  description?: string
  // False when this target can't select the domain — it doesn't publish it, or
  // a repository in requiresRepos isn't offered here. It is still sent, so the
  // UI shows it locked with the reason rather than hiding a capability that
  // exists on other targets.
  available: boolean
  unavailableReason?: string
  // Repository ids to enable alongside the pack's own when this domain is
  // picked. A metapackage can depend on packages published somewhere the pack
  // repository doesn't carry, and selecting it without that repository yields a
  // template that can't resolve at build time.
  requiresRepos?: string[]
  // A package can belong to more than one domain, so per-domain counts can sum
  // to more than the pack's distinct total — count unique names, never the sum.
  packages: EdgePackPackage[]
}

export interface EdgePackPackage {
  name: string
  description?: string
  // Absent when the pack's repository index was unreachable or doesn't carry
  // the package. The package stays selectable at "latest", which is what an
  // unpinned pick means anyway — only the version chips are lost.
  version?: string
  versions?: PackageVersion[]
}

export interface BuildAccepted {
  buildId: string
  status: string
  logsUrl: string
}

export interface Artifact {
  name: string
  type: 'image' | 'sbom'
  path: string
  size?: string
}

// Teardown-residue warning surfaced when a cancelled/failed build may have left
// the machine in a state needing manual cleanup. kind distinguishes a
// cancellation-failure (the cancel signal couldn't be delivered) from a
// cleanup-failure (ICT ran but reported leftover mounts/loop devices). detail
// carries the remediation hint (the failing kill error, or ICT's mount/loop lines).
export interface ResidualIssue {
  kind: 'cancellation-failure' | 'cleanup-failure'
  detail: string
}

// The server's six build states, verbatim (the BuildStatus schema in
// api/v1/openapi-template-builder.yaml). Shared by every component that reports
// or renders a build's lifecycle so a new state can't be handled in one place
// and silently dropped in another. 'idle' is a UI-only state meaning "no build
// owns the session right now".
export type BuildStatus =
  | 'idle'
  | 'not-started'
  | 'running'
  | 'cancelling'
  | 'cancelled'
  | 'success'
  | 'failed'

// isActiveStatus reports whether a build is still in flight — the server holds
// the single-build slot in exactly these states, so this is what gates starting
// another compose and what drives history polling.
export function isActiveStatus(s: string): boolean {
  return s === 'not-started' || s === 'running' || s === 'cancelling'
}

// Reproducibility/troubleshooting metadata for a build: the exact command that
// ran, the resolved template (+ a download URL), and the per-build directories.
export interface BuildDetails {
  buildId: string
  status: string
  command: string
  template: string
  templateUrl: string
  workDir: string
  cacheDir: string
  summary?: ComposeSummary
  hasLogFile: boolean
  errMsg?: string
  // Partial outputs left on disk after a fail/cancel (with on-disk path).
  artifacts?: Artifact[]
  // Teardown-residue warning, when cleanup left something behind.
  residual?: ResidualIssue
}

// One row in the compose history list.
export interface HistoryItem {
  id: string
  status: string
  template: string
  createdAt: string
  summary?: ComposeSummary
}

// Response to POST /builds/{id}/cancel. residual is present when the cancel was
// accepted but the signal could not be delivered — the build stays 'cancelling',
// so this is the only place that failure surfaces promptly.
export interface CancelAccepted {
  buildId: string
  status: string
  residual?: ResidualIssue
}

export interface BuildComplete {
  status: 'success' | 'failed' | 'cancelled'
  artifacts?: Artifact[]
  message?: string
  residual?: ResidualIssue
}
