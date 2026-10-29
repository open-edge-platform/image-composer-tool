# CLI Quick Start (Docker)

Image Composer Tool (ICT) builds custom Linux OS images, such as bootable RAW
disk images and ISO installers, from YAML templates. This guide builds an OS
image from a template you write, with the `image-composer-tool` command-line
tool running inside a Docker container, and puts the result in a directory on
your host. The host needs only Docker and a checkout of this repository: the
ICT container image carries the tool and every program a build runs, such as
`debootstrap`, `mkfs`, and `grub`.

> **Warning: the build container is privileged, and therefore root-equivalent
> on the host.** Building an OS image creates loop devices, mounts filesystems,
> and chroots into them, so the container runs with `--privileged` and the
> host's `/dev` mounted. Build only templates, and files they reference, that
> you trust.

## How It Works

`scripts/build-in-container.sh` runs on your host. It starts the container
with the mounts below and runs `image-composer-tool build` inside it. When the
build succeeds, it copies the OS image and its software bill of materials
(SBOM) into your output directory, owned by you rather than by root.

| On the host | Inside the container | Purpose |
|-------------|----------------------|---------|
| The template's directory, or `--context DIR` | `/input` (read-only) | The template and the files it references |
| Your output directory, created if missing | `/output`, or `--mount-point DIR` | Receives the OS image |
| Named volume `ict-cache`, or `--cache-volume NAME` | `/opt/ict/cache` | Downloaded packages, reused by later builds |
| Named volume `ict-workspace`, or `--workspace-volume NAME` | `/opt/ict/workspace` | The build's work tree, reused by later builds |

## Prerequisites

- A Linux x86_64 host with Git and Docker Engine. Your user must be allowed to
  run `docker`, which means being in the `docker` group; with `sudo` instead,
  the copied files belong to root.
- At least 15 GB of free disk space. The container image and Docker's build
  cache take about 5 GB, and a minimal RAW image needs about 5 GB more while it
  builds. Larger images, such as ISO installers, need more.
- Network access to the package repositories your image downloads from. Behind
  a proxy, read [Behind a Proxy](#behind-a-proxy) first.

## 1. Build the ICT Container Image

```bash
git clone https://github.com/open-edge-platform/image-composer-tool.git
cd image-composer-tool
docker build -f build/ict/Dockerfile --target ict -t ict .
```

The first build takes several minutes; later builds reuse Docker's cache. Run
it again after `git pull` so the image picks up the new commits.

## 2. Write a Template

A template is a YAML file that describes the OS image: the distribution, the
disk layout, the packages, the users, and so on. The quickest start is a copy
of a template shipped in `image-templates/`:

```bash
mkdir -p ~/my-image
cp image-templates/ubuntu24/ubuntu24-x86_64-minimal-raw.yml ~/my-image/my-image.yml
```

Edit `~/my-image/my-image.yml`, for example to rename the image and add a
package:

```yaml
image:
  name: my-image       # output files are named <name>-<version>.<format>
# ...
systemConfig:
  name: minimal        # the output directory is named after this
  # ...
  packages:
    # ...
    - vim              # added
```

The [template reference](../architecture/image-composer-tool-templates.md)
describes every field. To check the template before you build it:

```bash
docker run --rm -v ~/my-image:/input:ro ict image-composer-tool validate /input/my-image.yml
```

Every run of the container first prints `PREFLIGHT OK: ...`, its check that
every program ICT may run is present.

## 3. Build the OS Image

```bash
scripts/build-in-container.sh ~/my-image/my-image.yml ./out
```

The build log streams to your terminal; a minimal image takes several minutes
the first time and less afterwards. When the build succeeds, the script copies
its outputs into `./out`:

```text
out/
└── minimal/                                         # systemConfig.name
    ├── my-image-24.04.raw.gz                        # the disk image, gzip-compressed
    ├── my-image-24.04.vhdx                          # the same disk as a VHDX file
    └── spdx_manifest_deb_my-image_<timestamp>.json  # the SBOM
```

The template's `disk.artifacts` list decides which formats you get. ISO
templates also produce a second directory, holding the installer's initial RAM
disk. Paths in the build log are inside the container, where `./out` is
mounted at `/output`.

Arguments after `--` go to `image-composer-tool build`; run
`docker run --rm ict image-composer-tool build --help` to list them. A file
path among them is read inside the container, so give it as
`/input/<path relative to the context directory>`:

```bash
scripts/build-in-container.sh ~/my-image/my-image.yml ./out -- --hostname edge-01 --log-level debug
```

## Choose What Is Mounted

Pass `--mount-point` to mount your output directory somewhere other than
`/output` inside the container:

```bash
scripts/build-in-container.sh --mount-point /artifacts ~/my-image/my-image.yml ./out
```

The files still land in `./out` on the host; only the path inside the
container, which the build log shows, changes. The mount point must be an
absolute path other than `/`, and must not be or sit under `/input`, `/dev`,
or `/opt/ict`, where it would hide files the build needs.

Relative paths in a template resolve from its directory, as they do outside a
container, but the build sees only the mounted directory. If your template
references a file outside its own directory, for example an additional file at
`../files/motd`, mount a directory that contains everything with `--context`:

```bash
scripts/build-in-container.sh --context ~/templates ~/templates/edge/my-image.yml ./out
```

Some templates in `image-templates/` reference files elsewhere in the
checkout, such as under `config/`; build those with `--context .` from the
checkout's root.

## Behind a Proxy

Export your proxy settings before both steps. `docker build` takes them as
build arguments (a bare `--build-arg NAME` passes your shell's value), and the
script passes them into the container:

```bash
export http_proxy=http://proxy.example.com:911
export https_proxy=http://proxy.example.com:912
export no_proxy=localhost,127.0.0.1
docker build --build-arg http_proxy --build-arg https_proxy --build-arg no_proxy \
  -f build/ict/Dockerfile --target ict -t ict .
scripts/build-in-container.sh ~/my-image/my-image.yml ./out
```

## Stop, Reuse, and Clean Up

- **Stop a build** with Ctrl+C. ICT unmounts its filesystems and detaches its
  loop devices, and nothing is copied. Do not `docker kill` the container: a
  build killed without that cleanup can leave mounts and loop devices behind
  on the host.
- **Reuse the web UI's image.** If you run the web UI with `docker compose`,
  Compose built the same container image as `<project>-ict`, by default
  `image-composer-tool-ict`. Pass `--image image-composer-tool-ict` to use it.
- **Reclaim disk space** taken by downloaded packages and work trees with
  `docker volume rm ict-cache ict-workspace`.

## Troubleshooting

| Symptom | Fix |
|---------|-----|
| `image 'ict' not found` | Build it as in [step 1](#1-build-the-ict-container-image), or pass the name of the one you have with `--image`. |
| A file the template references is not found | It is outside the mounted directory. Pass a `--context` that contains it, as in [Choose What Is Mounted](#choose-what-is-mounted). |
| The build fails while resolving or downloading packages | The log names the repository it could not reach. Behind a proxy, check that your proxy variables are exported and that `no_proxy` does not send that host around the proxy. |

## Next Steps

- [Building an Image in a Docker Container](./usage-guide.md#building-an-image-in-a-docker-container)
  shows the `docker run` the script wraps, to run a build without it.
- [Web UI Quick Start](./web-ui-quick-start.md) builds OS images from a browser,
  with the same container image.
- [Quick Start](./quick-start.md) builds an image with the CLI installed on the
  host, without Docker.
