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
  imageType: string
}

export interface ComposeSummary {
  imageName: string
  vertical: string
  sku: string
  platform: string
  os: string
  imageType: string
  packageCount: number
  diskSize: string
  partitionCount: number
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
}

export interface BuildComplete {
  status: 'success' | 'failed'
  artifacts?: Artifact[]
  message?: string
}
