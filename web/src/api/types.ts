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
}

// One row in the compose history list.
export interface HistoryItem {
  id: string
  status: string
  template: string
  createdAt: string
  summary?: ComposeSummary
}

export interface BuildComplete {
  status: 'success' | 'failed'
  artifacts?: Artifact[]
  message?: string
}
