# ADR: Native Container Support

**Status**: Proposed
**Date**: 2026-07n-22
**Updated**: N/A
**Authors**: OS Image Composer Team
**Technical Area**: Container Support

---

## Summary

ICT will add an optional top-level containers section to the image template
schema. The section will allow users to declare one or more container artifacts
that must be included in the composed system image.

Each container entry will support one of two mutually exclusive sources:

A prebuilt container image referenced by registry name and preferably an
immutable digest. A container image built from a local or remote build context
and Dockerfile.

ICT will acquire or build the container images during the image composition
process and store each result in a runtime-independent OCI-compatible archive
inside the target filesystem. The archive will be accompanied by immutable
digest and provenance metadata.

For the initial implementation, ICT will not directly construct or modify
Docker’s internal runtime data directory, such as /var/lib/docker. Instead, ICT
will install a deterministic systemd import service in the target image. The
service will import the embedded OCI archives into the selected container
runtime during the first boot, verify the resulting image digest, and then mark
the import as complete.

Where a provider and runtime can safely support offline import during
composition, ICT may import the image into the runtime store before finalizing
the image. However, this will be an implementation optimization rather than the
portable contract of the template.

An optional lifecycle policy will allow the user to request that a container or
Docker Compose application be started after its images have been imported.
Automatic startup will be opt-in rather than the default.

When containers are declared, ICT will validate that a supported container
runtime is available in the final system image. Depending on the selected policy,
ICT will either automatically add the provider-defined runtime packages or fail
validation with a clear error.

This design allows ICT to evolve from composing package-based operating systems
into composing complete edge software stacks while retaining reproducibility,
provider separation, and a declarative template model.

## Context

ICT currently composes bootable operating-system images primarily from Debian
and RPM packages. Users describe the target operating system, package selection,
disk layout, users, services, files, and build-time configuration in a YAML
template. ICT merges that template with an operating-system-specific default and
validates the merged result against its JSON Schema before beginning composition.

The ICT build pipeline already separates template validation, package
acquisition, composition, signing, and finalization. During composition, ICT
creates or reuses an operating-system-specific chroot environment, mounts the
required pseudo-filesystems, installs packages, executes configuration logic,
enables services, and cleans up the environment before producing the final
artifact.

This architecture is suitable for operating-system composition but does not yet
provide a first-class mechanism for including application artifacts delivered as
container images.

Edge systems, particularly systems hosting Agentic AI, inference, data-processing,
and orchestration stacks, increasingly require a combination of:

- Traditional Debian or RPM packages.
- A container runtime.
- One or more prebuilt container images.
- Images built from a product-specific Dockerfile.
- Multi-container application definitions such as Docker Compose.
- Configuration files, models, application assets, and other artifacts.

Treating these components as post-deployment operations creates a gap between
the system image ICT validates and the system that eventually runs at the edge.
It also introduces runtime dependence on an external registry and makes 
deployment less deterministic.

### Problem Statement

ICT has no declarative, schema-validated way to express that one or more
container images form part of the composed edge system.

Users can currently add a container runtime package or invoke arbitrary scripts,
but that approach has several limitations:

- The template does not express container artifacts as first-class image content.
- ICT cannot validate that a runtime is installed when containers are requested.
- ICT cannot distinguish between pulling a prebuilt image and building an image
from source.
- Container image digests and provenance are not captured in the image manifest.
- Proxy configuration is not consistently propagated to container acquisition
and build operations.
- Docker Compose dependencies and service policies cannot be validated.
- Container artifacts are not included in ICT caching, SBOM, inspection,
comparison, or reproducibility workflows.
- A target may require network access on its first boot to acquire images that
should already have been included during composition.
- Arbitrary post-install scripts make cleanup and failure handling difficult.

ICT therefore needs a controlled mechanism for acquiring or building container
images and embedding them in the composed system image before deployment.

### Background

There are two primary container-image workflows that ICT must support.

#### Prebuilt image workflow

A user identifies an existing image in a registry, for example:

docker.io/library/nginx@sha256:...

ICT pulls the image, verifies its digest when specified, stores it in the
composed image, and records its identity in the build manifest.

Tags may be accepted for development use, but immutable digests should be
strongly recommended and may be required when strict reproducibility is enabled.

#### Dockerfile build workflow

A user supplies:

- A local or remote build context.
- A Dockerfile path.
- Optional build arguments.
- Optional target stage.
- Optional secrets or SSH forwarding policy.
- An expected output image name.
- Optionally, an expected resulting digest.

ICT runs the container build during composition rather than requiring the target
to build or download the image after deployment.

Building the image during ICT composition provides several benefits:

- The complete software stack is known before deployment.
- Build failures occur in the controlled ICT environment.
- Registry access is not required at the target.
- Images can be scanned and represented in the final SBOM.
- The image can be tied to the system-image manifest.
- Edge installation becomes more deterministic and less dependent on network
availability.

However, container-image construction and runtime-store population must be
treated as separate concerns. A Docker or containerd runtime’s internal storage
is not a stable interchange format. Its layout can depend on runtime version,
snapshotter, storage driver, backing filesystem, mount topology, and daemon
configuration.

For that reason, ICT must use OCI-compatible artifacts as the portable handoff
format rather than attempting to manually generate Docker’s internal data
directory.

### Existing Infrastructure

ICT already contains the major architectural building blocks needed for this
feature:

1. Template loading and merging
User templates are merged with provider and image-type-specific defaults. The
merged template is validated before the build begins.

1. JSON Schema validation
ICT uses JSON Schema validation and rejects unsupported or incompatible template
configurations before composition. The current Go implementation uses
github.com/santhosh-tekuri/jsonschema/v5.

1. Provider-specific default configurations
Default templates and operating-system-specific behavior are maintained beneath
the provider configuration hierarchy. These defaults already supply essential
packages and image-type-specific configuration.

1. Package resolution and caching
ICT resolves package dependencies, downloads artifacts, verifies them, and
reuses cached package content between builds.

1. Reusable chroot environments
ICT creates or restores a provider-specific chroot environment, mounts
/proc, /sys, and /dev, and executes installation and configuration operations
within that environment.

1. Build-time customizations
ICT already supports pre-installation and post-installation operations,
copying files, configuring services, and running provider-specific lifecycle
logic.

1. Final manifest and SBOM generation
ICT generates final image metadata and an SPDX SBOM during finalization.
Container artifacts can extend these existing outputs rather than creating a
separate reporting mechanism.

The container implementation should extend these existing stages instead of
adding a parallel image-composition workflow.

## Decision / Recommendation

### 1. Add an optional top-level containers section

The template will support an optional top-level object named containers, holding
runtime, images, and applications sections. The images field is an array rather
than a single container object because an edge stack commonly contains multiple
independently built or acquired images.

Example:

```yaml
containers:
  runtime:
    type: docker
    installPolicy: auto

  images:
    - name: inference-service
      source:
        type: build
        context: ./containers/inference
        dockerfile: Dockerfile
      image:
        repository: localhost/edge/inference
        tag: "1.0.0"
      build:
        target: runtime
        args:
          MODEL_VARIANT: small
      lifecycle:
        autostart: false

    - name: metrics-agent
      source:
        type: prebuilt
        reference: docker.io/example/metrics-agent@sha256:0123456789abcdef...
      lifecycle:
        autostart: true
        restartPolicy: unless-stopped
        command:
          - "--config"
          - "/etc/metrics/config.yml"
```

Although containers is top-level, its content should distinguish between:

- Runtime requirements.
- Container image artifacts.
- Application or service activation policy.

This avoids mixing runtime installation policy into every image entry.

### 2. Support two mutually exclusive source types

Every containers.images[] entry must select exactly one source type.

#### Prebuilt source

```yaml
source:
  type: prebuilt
  reference: registry.example.com/team/service@sha256:...
```

Supported fields should include:

- reference
- platform
- expectedDigest
- registryAuthRef
- tlsVerify
- pullPolicy

Recommended initial pullPolicy values:

- if-not-cached
- always
- never

A reference containing a digest should be preferred. A mutable tag without a
digest should produce a warning, or an error when reproducible-build policy is
enabled.

#### Build source

```yaml
source:
  type: build
  context: ./services/inference
  dockerfile: Containerfile
```

Supported fields should include:

- context
- dockerfile
- contextType
- revision
- checksum
- ignoreFile
- network
- platform

The build configuration may additionally contain:

```yaml
build:
  args:
    HTTP_PROXY: "${HTTP_PROXY}"
  target: runtime
  labels:
    org.opencontainers.image.source: "..."
  noCache: false
```

Exactly one of prebuilt or build must be selected. Schema-level oneOf validation
should enforce the distinction.

### 3. Use a runtime-independent OCI artifact as the canonical output

ICT will treat an OCI image archive or OCI image layout as the canonical
composed container artifact.

Recommended target path:

`/usr/lib/ict/containers/images/<logical-name>/`

Each image directory should contain, as applicable:

image.oci.tar
metadata.json
digest
source.json

An index should be generated at:

`/usr/lib/ict/containers/images.lock`

For example:

```json
{
  "schemaVersion": 1,
  "images": [
    {
      "name": "inference-service",
      "runtimeReference": "localhost/edge/inference:1.0.0",
      "digest": "sha256:...",
      "artifact": "images/inference-service/image.oci.tar",
      "sourceType": "build"
    }
  ]
}
```

The lock file becomes the authoritative mapping between the declarative template
and the exact container artifacts embedded in the system image.

This location is system-owned, read-only during normal operation, and
independent of a particular user home directory.

Large container content must be accounted for in disk sizing. ICT should
calculate the combined archive size and apply a configurable expansion factor
before filesystem finalization. A build must fail early when the requested image
cannot fit in the target root filesystem.

### 4. Do not directly generate Docker’s internal data store

ICT will not directly copy files into or manufacture:

`/var/lib/docker`

or another runtime’s internal storage hierarchy as the portable implementation.

Doing so would couple the output image to:

- Docker or containerd version.
- Storage driver.
- Snapshotter.
- Backing filesystem.
- Kernel capabilities.
- SELinux or AppArmor labeling.
- Overlay filesystem behavior.
- Runtime daemon configuration.

Instead, ICT will embed an OCI-compatible artifact and use the selected
runtime’s supported import command.

### 5. Import images through a deterministic systemd service

For the portable initial implementation, ICT will install a systemd oneshot
service such as:

ict-container-import.service

The service will:

- Run after the container runtime is available.
- Read /usr/lib/ict/containers/images.lock.
- Check whether the expected digest is already present.
- Import missing images using the selected runtime.
- Verify the imported image digest.
- Write a versioned completion marker.
- Fail visibly if an import or verification operation fails.
- Avoid downloading content from a registry.
- Be idempotent across reboots.
- Run before any ICT-managed container service.

Example ordering:

After=docker.service
Requires=docker.service
Before=ict-container-apps.target

A completion marker should include the lock-file digest rather than merely
indicating that the service has run:

`/var/lib/ict/container-import/<lock-digest>.complete`

If the embedded lock file changes in a newer system image, the import service
will run again.

Provider implementations may import into the runtime store during composition
where that can be done safely. However, the systemd import path remains the
baseline portable behavior.

### 6. Build container images in the target chroot through a dedicated builder abstraction

ICT will introduce a container artifact builder interface separate from the
existing OS provider lifecycle interface.

Conceptually:

```golang
type ContainerArtifactBuilder interface {
    Validate(
        ctx context.Context,
        spec ContainerImageSpec,
        target Target,
    ) error

    AcquireOrBuild(
        ctx context.Context,
        rootfs string,
        spec ContainerImageSpec,
        proxy ProxyConfig,
    ) (ContainerArtifact, error)

    Inspect(
        ctx context.Context,
        artifact ContainerArtifact,
    ) (ContainerMetadata, error)
}
```

The first implementation should use a daemonless OCI build mechanism wherever
possible.

Preferred order:

1. BuildKit in daemonless mode.
1. Buildah or Podman where supported by the target distribution.
1. Docker daemon execution only when the provider explicitly supports and tests it.

The build command should execute with the target chroot as the user-space
environment so that:

- The build tooling and certificates are those defined for that target.
- Provider proxy and trust-store configuration is honored.
- The resulting behavior is reproducible for that distribution.
- The feature integrates naturally with the existing compose stage.

However, ICT should not assume that a normal dockerd process can always run
inside a chroot. Container builds may require namespaces, cgroups, overlay mounts,
networking, and daemon cleanup that a basic chroot does not provide.

The implementation should therefore define “build inside the chroot” as:

Execute target-distribution container build tooling against the mounted target
root filesystem, with explicitly provisioned pseudo-filesystems, namespaces,
temporary state directories, and cleanup managed by ICT.

This may involve:

- Bind-mounting the build context into the chroot.
- Mounting /proc, /sys, /dev, and /dev/pts.
- Providing a temporary build state directory outside the final rootfs.
- Running daemonless BuildKit with OCI archive output.
- Passing an explicit target platform.
- Unmounting all build mounts during normal completion, cancellation, and failure.
- Deleting transient layers and sockets.
- Never retaining build credentials in the target image.

Container building should be a new operation within the compose stage, after the
required target packages and trust configuration have been installed but before
final cleanup, manifest generation, and SBOM generation.

### 7. Retain per-OS chroot package lists

ICT will continue maintaining a provider or distribution-specific chroot package
list. The container feature must not introduce a single universal package list
because package names, supported tools, and runtime integrations vary by
distribution.

Each provider should define capabilities such as:

```yaml
containerBuild:
  supported: true
  builder: buildkit
  packages:
    - buildkit
    - runc
    - ca-certificates
    - tar

containerRuntime:
  supported:
    - docker
    - containerd
  docker:
    packages:
      - docker-ce
      - docker-ce-cli
      - containerd.io
```

The exact representation may remain Go configuration or provider YAML rather
than being exposed in the user template. The user declares intent. The provider
resolves that intent into concrete package names.

Build-only packages do not necessarily need to remain in the final system image.
ICT should distinguish between:

- Chroot build dependencies: Needed only while ICT builds or transforms container
artifacts.
- Target runtime dependencies: Required in the deployed image.
- Application dependencies: Required by the container application outside the
container image.

Where practical, build-only packages should be installed in the reusable build
environment or removed from the final root filesystem before finalization.

### 8. Add explicit container-runtime policy

The containers.runtime section should support:

```yaml
containers:
  runtime:
    type: docker
    installPolicy: auto
```

Suggested values:

`type`

Initial values:

- docker
- containerd
- podman

The first release may implement only docker, but the schema and internal model
should avoid Docker-specific naming where it is not required.

`installPolicy`

- auto: Add provider-recommended runtime packages when absent.
- require: Fail validation unless the user’s package selection or provider
default already includes the runtime.
- none: Embed OCI artifacts but do not install a runtime or import service.

The default should be auto when containers.images is non-empty.

The merged-template validator must verify that the selected runtime is
compatible with the provider and target image type.

For example, container support may initially be rejected for:

- WSL2 images, unless explicitly implemented.
- Initrd-only images.
- Read-only or dm-verity configurations that do not provide a writable runtime
data partition.
- Unsupported architectures.
- Filesystems incompatible with the selected runtime storage driver.

### 9. Make automatic startup opt-in

Merely adding an image must not cause the image to execute.

The default will be:

```yaml
lifecycle:
  autostart: false
```

When autostart: true, ICT will generate a managed systemd unit or Compose unit.

A single container might be expressed as:

```yaml
lifecycle:
  autostart: true
  restartPolicy: unless-stopped
  environmentFiles:
    - /etc/inference/environment
  volumes:
    - source: /var/lib/models
      target: /models
      readOnly: true
  ports:
    - "8080:8080"
```

ICT should generate service definitions rather than executing docker run during
composition. Generated services must start only after ict-container-import.service
succeeds.

The initial implementation should support a deliberately constrained lifecycle
subset rather than attempting to reproduce the entire Docker CLI schema.

### 10. Treat Docker Compose as an application definition

Docker Compose support should be modeled separately from individual image
acquisition.

Example:

```yaml
containers:
  runtime:
    type: docker
    installPolicy: auto

  images:
    - name: api
      source:
        type: build
        context: ./api
        dockerfile: Dockerfile
      image:
        repository: localhost/edge/api
        tag: "1.0"

    - name: database
      source:
        type: prebuilt
        reference: docker.io/library/postgres@sha256:...

  applications:
    - name: agentic-stack
      type: compose
      file: ./deploy/compose.yml
      autostart: true
```

ICT should:

- Copy the Compose file into a managed location.
- Parse and validate the file.
- Ensure locally referenced images are declared in containers.images.
- Reject or warn about undeclared registry images.
- Rewrite or resolve image references to the locally embedded names where necessary.
- Generate a systemd service for docker compose up.
- Ensure the service runs after image import.
- Never execute the application during image composition.

Initial Compose support should exclude:

- build: directives inside the Compose file, unless explicitly mapped to ICT
container entries.
- Swarm-only options.
- External secrets fetched at composition time.
- External configs not included in the template.
- Privileged services unless explicitly allowed by policy.
- Host-path mounts whose target paths are absent or incompatible.
- Automatic registry pulls at first boot.

A later release may support converting Compose build: entries into ICT build
requests.

### 11. Propagate proxy configuration consistently

When ICT proxy settings are configured, they must be propagated to all
network-facing container operations.

This includes:

- Registry image pulls.
- Dockerfile base-image resolution.
- Package downloads inside Dockerfile build stages.
- Remote Git build contexts.
- BuildKit frontend and gateway operations.
- Certificate and registry authentication access.

ICT should propagate both uppercase and lowercase forms where required:

- HTTP_PROXY
- HTTPS_PROXY
- NO_PROXY
- http_proxy
- https_proxy
- no_proxy

Proxy configuration must be separated into:

1. Build-time proxy configuration
Available to the builder and, when permitted, Dockerfile build stages.

1. Target runtime proxy configuration
Installed as a runtime service drop-in when the deployed Docker daemon must use
a proxy.

1. Application proxy configuration
Passed into running containers only when explicitly requested by the application
configuration.

1. ICT must not automatically bake proxy credentials into container-image layers.
Where BuildKit is used, proxy values should be passed through its predefined
proxy build arguments or secret mechanisms rather than being added through
persistent ENV instructions.

The NO_PROXY value should automatically include local runtime endpoints and
registries where appropriate, including loopback addresses and any configured
local registry endpoint.

Sensitive proxy URLs must be redacted from logs and excluded from the final
manifest.

### 12. Capture container metadata in manifests and SBOMs

The final ICT image manifest should include:

- Logical container name.
- Runtime reference.
- Source type.
- Registry source or build-context identifier.
- Source revision or checksum.
- Dockerfile checksum.
- Image config digest.
- Image manifest digest.
- Platform.
- Creation timestamp, when present.
- Builder implementation and version.
- OCI archive path.
- Whether autostart is enabled.
- Compose application membership.

Container package content should be represented in the SBOM where supported.

At minimum, ICT must include an SBOM relationship between:

- The composed system image.
- Each embedded OCI container image.
- A child SBOM for each container image, when generated or supplied.

A failure to generate a detailed container SBOM may initially be a warning, 
but failure to determine and record the image digest must be fatal.

### Core Design Principles

#### Declarative behavior

Container content must be expressed in the template rather than hidden in arbitrary post-install scripts.

#### Optional and backward compatible

The containers section is optional. Existing templates without it must continue to behave exactly as before.

#### Runtime-independent artifact boundary

OCI artifacts are the portable boundary between ICT container composition and runtime-specific import.

#### Reproducibility by default

Immutable digests, checksums, pinned revisions, and captured build metadata should be encouraged. Strict mode should reject unpinned remote inputs.

#### Provider ownership of distribution details

The common ICT image-build layer owns the container workflow. Providers supply package names, supported builders, runtime capabilities, and target-specific preparation.

#### No duplicated provider implementation

Container orchestration should not be reimplemented separately in every provider. Common logic should live in the shared image-composition layer, with provider capability hooks where operating-system behavior differs.

#### Explicit lifecycle policy

Embedding a container image and running a container are distinct actions. Startup is never implied merely by image inclusion.

#### Safe cancellation and cleanup

All mounts, namespaces, sockets, subprocesses, temporary build roots, credentials, and daemon state must be tracked and cleaned when a build succeeds, fails, or is cancelled.

#### Least privilege

Build operations should use the minimum capabilities required. Privileged container builds, host networking, device access, and arbitrary bind mounts should require explicit policy.

#### Observable outputs

ICT logs, manifests, inspection, comparison, and SBOM results must expose the container artifacts included in the image.

## Proposed Schmea Direction

The exact schema naming can be adjusted during implementation, but the following
represents the recommended logical structure:

```yaml
containers:
  runtime:
    type: docker
    installPolicy: auto
    importOnBoot: true

  policy:
    requireDigests: true
    allowNetworkBuild: true
    allowPrivilegedBuild: false
    allowHostNetwork: false
    allowMutableTags: false

  images:
    - name: inference-service

      source:
        type: build
        context: ./containers/inference
        dockerfile: Dockerfile
        revision: null
        checksum: null

      image:
        repository: localhost/edge/inference
        tag: "1.0.0"

      build:
        target: runtime
        platform: linux/amd64
        args:
          MODEL_VARIANT: small
        secrets:
          - id: registry-token
            source: environment
            key: REGISTRY_TOKEN

      validation:
        expectedDigest: null
        scan: true
        generateSbom: true

      lifecycle:
        autostart: false
        restartPolicy: unless-stopped

  applications:
    - name: agentic-stack
      type: compose
      file: ./deploy/compose.yml
      autostart: true
```

Not every field needs to be included in the first release. The initial schema
should be intentionally constrained while preserving this separation of concerns.

The first implementation should support:

- An optional containers top-level section.
- One or more container image entries.
- Docker as the first runtime.
- Prebuilt images referenced by OCI/Docker registry reference.
- Local Dockerfile and local build context.
- OCI archive output.
- Digest capture and verification.
- Provider-specific runtime and builder package resolution.
- Proxy propagation.
- Deterministic first-boot import.
- Optional single-container autostart.
- Basic Docker Compose file installation and autostart, provided all images are
predeclared.
- Manifest integration.
- Basic SBOM integration.
- Build cancellation and complete cleanup.

The first implementation should not support:

- Hosting or managing a local registry.
- Pushing images to a registry.
- Kubernetes manifests or Helm.
- Arbitrary remote HTTP build contexts.
- Unrestricted Git credentials.
- Multi-node builds.
- Cross-architecture emulation unless already supported and explicitly enabled.
- Arbitrary Docker daemon configuration.
- Runtime storage-driver customization.
- Automatically executing Compose applications during composition.
- Building from Compose build: directives without corresponding ICT image entries.
- Secret persistence in the final image

## Separation of Responsibilities

TBD

## Risks and Mitigations

**Risk:** Running a container daemon inside a chroot is unreliable
A standard chroot does not itself provide the namespaces, cgroups, mount
propagation, and runtime environment expected by Docker.

**Mitigation:**

- Prefer daemonless BuildKit or Buildah.
- Isolate the builder behind an interface.
- Make daemon-based builders provider-specific.
- Explicitly provision and clean required mounts and namespaces.
- Never assume that systemd is running in the chroot.

**Risk:** Runtime data-store coupling
Writing directly to /var/lib/docker can produce images dependent on a particular
Docker version, storage driver, or backing filesystem.

**Mitigation:**

- Use OCI archive/layout as the canonical artifact.
- Import using supported runtime commands.
- Treat composition-time runtime import as an optional optimization.

**Risk:** First-boot import increases initial startup time
Large images may take significant time and require additional temporary disk
space during import.

**Mitigation:**

- Clearly report embedded and estimated expanded sizes.
- Size the target filesystem accordingly.
- Make the import observable through systemd status.
- Allow provider-specific composition-time import where safe.
- Consider a future OCI-native runtime storage implementation.

**Risk:** Duplicate storage
The target may temporarily contain both OCI archives and unpacked runtime layers.

**Mitigation:**

- Add a policy such as retainArchives: true|false.
- Default to retaining archives for immutable or recovery-oriented systems.
- Permit deletion after verified import on writable systems.
- Account for worst-case storage during validation.

**Risk:** Non-reproducible remote inputs
Mutable tags, Git branches, and unpinned base images can change between builds.

**Mitigation:**

- Prefer digest references.
- Require pinned revisions in strict mode.
- Record resolved base-image digests.
- Record context and Dockerfile checksums.
- Add a reproducibility policy to the template.

**Risk:** Credential leakage
Registry, proxy, Git, or build credentials could be written into logs, layers,
caches, or the final image.

**Mitigation:**

- Use named secret references rather than literal values.
- Use BuildKit secret mounts.
- Redact command-line and environment output.
- Exclude secret values from cache metadata.
- Scan build outputs and temporary directories.
- Fail builds where safe secret delivery is unavailable.

**Risk:** Build context accesses unintended host files
A malicious or incorrect context may include symlinks or paths outside the
intended directory.

**Mitigation:**

- Canonicalize paths.
- Reject path traversal.
- Validate symlink targets.
- Bind-mount contexts read-only.
- Apply context-size limits.
- Honor .dockerignore.
- Reject special files by default.

**Risk:** Increased build time and storage consumption
AI and edge container images can contain many gigabytes of layers.

**Mitigation:**

- Add content-addressed caching.
- Stream downloads and exports.
- Deduplicate by digest.
- Support offline cache reuse.
- Report size before finalization.
- Fail early when disk capacity is inadequate.

**Risk:** Unsupported target kernel or filesystem
The target runtime may require capabilities not present in the selected kernel or filesystem.

**Mitigation:**

- Add provider capability checks.
- Validate required kernel options where possible.
- Validate writable runtime storage.
- Document supported combinations.
- Fail before final image creation.

**Risk:** Container autostart creates security exposure
Automatically running containers may expose ports, mount host paths, or run with excessive privileges.

**Mitigation:**

- Default autostart to false.
- Disallow privileged mode by default.
- Require explicit port, device, and volume declarations.
- Add policy validation.
- Generate hardened systemd units where possible.

**Risk:** OCI and OS SBOMs become disconnected
A traditional package SBOM may omit software packaged inside container layers.

**Mitigation:**

- Generate per-container SBOMs.
- Link them to the system-image SBOM.
- Include image digests and relationships in the manifest.
- Make SBOM-generation failures visible.

**Risk:** Reused chroot contaminates subsequent builds
Container builder caches, credentials, mounts, and daemon state may persist in ICT’s reusable chroot.

**Mitigation:**

- Keep builder state outside the reusable chroot.
- Use per-build state directories.
- Mount credentials ephemerally.
- Verify cleanup.
- Add contamination regression tests.
- Invalidate or rebuild the chroot after unrecoverable cleanup failures.

**Risk:** Cancellation leaves mounts or daemons active
A cancelled build could leave loop devices, namespaces, overlay mounts, sockets,
or child processes behind.

**Mitigation:**

- Track every acquired resource in a cleanup stack.
- Use context cancellation for subprocesses.
- Terminate process groups.
- Unmount in reverse order.
- Detect busy mounts.
- Treat incomplete cleanup as a build failure requiring workspace repair.

## Alternatives Considered

### Build directly into the runtime store at composition time

Use a daemonless builder (Buildah or Podman) to write the built or pulled image
straight into the runtime's own store (for example containers-storage) during
composition, so the image is present at first boot with no import step.

- Pro: fully offline, with zero first-boot work and the simplest boot path.
- Con: couples the composed system image to one runtime and to its
  storage-driver, snapshotter, and version specifics. This is the same coupling
  that section 4 rejects for /var/lib/docker.

Rejected as the portable contract because it ties the image to a single runtime.
It is retained only as the optional composition-time import optimization
described in section 5, available where a provider and runtime can do it safely.
The runtime-independent OCI archive plus first-boot import (the decision above)
is preferred because it keeps the runtime choice open and the artifact portable,
accepting one deterministic, offline first-boot import step as the cost.

### Generate the runtime data directory directly

Manufacture /var/lib/docker (or another runtime's internal storage hierarchy)
during composition so the runtime finds its images already unpacked at boot.

- Con: the internal layout depends on runtime version, storage driver,
  snapshotter, backing filesystem, mount topology, and daemon configuration, so
  the output image is not portable across any of those.

Rejected. This is the coupling documented in section 4; it is recorded here as a
formal alternative so that section reads as a decision rather than only a
prohibition.

### Defer all image acquisition to first boot (status quo)

Install only a runtime and pull images from a registry on the device at first
boot, as templates can already arrange today.

- Con: requires network access at first boot, is non-deterministic, and captures
  no digest or provenance in the image manifest.

Rejected as the baseline. Removing this first-boot network dependency and making
the embedded content deterministic is the motivation for this ADR.

## Final Recommendation

Proceed with an optional top-level containers schema and implement container
images as first-class ICT artifacts.

Use a shared container composition workflow with provider-supplied capabilities
and package mappings. Build or acquire images during the compose stage, export
them as OCI-compatible artifacts, embed them in the target filesystem, record
immutable digests and provenance, and import them through a deterministic
runtime-specific service.

Do not make Docker’s internal storage layout the architectural contract. Do not
automatically start containers simply because their images are included. Keep
local registry deployment outside the initial scope.

The first implementation should target Docker on Ubuntu 24 x86_64 as the
reference path, while keeping the data model and interfaces runtime-neutral
enough to support containerd and Podman later.
