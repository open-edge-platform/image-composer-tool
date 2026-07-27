// Types mirroring api/v1/openapi-template-builder.yaml (hand-written for the
// Basic slice; can be replaced with openapi-typescript codegen later).

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

// Teardown-residue warning surfaced when a cancelled/failed build may have left
// the machine in a state needing manual cleanup. kind distinguishes a
// cancellation-failure (the cancel signal couldn't be delivered) from a
// cleanup-failure (ICT ran but reported leftover mounts/loop devices). detail
// carries the remediation hint (the failing kill error, or ICT's mount/loop lines).
export interface ResidualIssue {
  kind: 'cancellation-failure' | 'cleanup-failure'
  detail: string
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

export interface BuildComplete {
  status: 'success' | 'failed' | 'cancelled'
  artifacts?: Artifact[]
  message?: string
  residual?: ResidualIssue
}
