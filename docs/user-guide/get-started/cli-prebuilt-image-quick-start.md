# CLI Quick Start (Prebuilt Container Image)

This guide runs the `image-composer-tool` command line from a prebuilt ICT
container image, without a checkout of this repository and without building
anything. Two shell functions make the container work like an installed CLI:

- `ict-build` builds an OS image from a template on your host and puts the
  result in a directory on your host.
- `ict` runs any other command: `validate`, `resolve`, `inspect`, `compare`,
  `cache`, `config`, `ai`, `serve`, `completion`, and `version`.

> **Warning: the container is privileged, and therefore root-equivalent on the
> host.** Building an OS image creates loop devices, mounts filesystems, and
> chroots into them, so the container runs with `--privileged` and the host's
> `/dev` mounted. Build only templates, and files they reference, that you
> trust.

## The Images

The hosted ICT service publishes three tags of the ICT container image, built
from [`build/ict/Dockerfile`](../../../build/ict/Dockerfile). The registry is
reachable from the Intel network only, and pulling needs no login.

| Image | What it is |
|-------|------------|
| `gar-registry.caas.intel.com/image-composer-tool-saas/ict:latest` | The image the service's build farm builds with |
| `gar-registry.caas.intel.com/image-composer-tool-saas/ict:latest-staging` | The image of the service's staging environment |
| `gar-registry.caas.intel.com/image-composer-tool-saas/ict:latest-dev` | The image of the service's development environment, the newest of the three. It can carry fixes not yet merged into `main` |

Each holds `image-composer-tool` with the web UI compiled in, `live-installer`
for ISO builds, the OS defaults (`config/`), the shipped templates
(`image-templates/`), and every program a build runs. The tags move: each
push replaces what a tag names. To stay on one image, see
[Choose, Pin, and Update the Image](#choose-pin-and-update-the-image).

Away from the Intel network, build the image yourself as in the
[CLI Quick Start (Docker)](./cli-docker-quick-start.md); this guide then works
with `ICT_IMAGE=ict`.

## Prerequisites

- A Linux x86_64 host with Docker Engine, `bash`, and `curl`. Your user must be
  in the `docker` group: the functions below run `docker` without `sudo`.
- At least 12 GB of free disk space. The image takes about 1.1 GB, and
  building a minimal RAW image takes about 9 GB more, including the compressed
  copy you receive. Larger images, such as ISO installers, need more.
- Network access to `gar-registry.caas.intel.com`, to
  `raw.githubusercontent.com` (`ict-build` downloads its script from there),
  and to the package repositories your image downloads from. Behind a proxy,
  read [Behind a Proxy](#behind-a-proxy) first.

## 1. Set Up the Commands

Paste this into a `bash` shell, or add it to `~/.bashrc` to keep it:

```bash
# The ICT container image: latest, latest-staging or latest-dev.
export ICT_IMAGE=gar-registry.caas.intel.com/image-composer-tool-saas/ict:latest

# ict-build [OPTIONS] TEMPLATE OUTPUT_DIR [-- BUILD_FLAGS...]
ict-build() {
    local script
    docker image inspect "$ICT_IMAGE" >/dev/null 2>&1 || docker pull "$ICT_IMAGE" || return
    script="$(curl -fsSL https://raw.githubusercontent.com/open-edge-platform/image-composer-tool/main/scripts/build-in-container.sh)" || return
    bash -c "$script" build-in-container.sh --image "$ICT_IMAGE" \
        --cache-volume "${ICT_VOLUMES:-ict}-cache" \
        --workspace-volume "${ICT_VOLUMES:-ict}-workspace" "$@"
}

# ict COMMAND [ARGS...]: image-composer-tool, with the current directory at /work
ict() {
    local stdin=""
    [ -t 0 ] && stdin=-i
    docker run --rm --init $stdin --privileged -v /dev:/dev --network host \
        -v "$PWD:/work" \
        -v "${ICT_VOLUMES:-ict}-cache:/opt/ict/cache" \
        -v "${ICT_VOLUMES:-ict}-workspace:/opt/ict/workspace" \
        -v "${ICT_VOLUMES:-ict}-webui-workspace:/opt/ict/webui-workspace" \
        -e http_proxy -e https_proxy -e no_proxy -e HTTP_PROXY -e HTTPS_PROXY -e NO_PROXY \
        -e OPENAI_API_KEY -e ICT_OEP_INSTALLER_TOKEN -e ICT_OEP_INSTALLER_TOKEN_FILE \
        --entrypoint image-composer-tool "$ICT_IMAGE" "$@"
}
```

Then check that they work. The first command pulls the image:

```bash
ict version
```

What the functions do:

- **`ict-build`** pulls `ICT_IMAGE` if you do not have it yet, downloads
  [`scripts/build-in-container.sh`](../../../scripts/build-in-container.sh)
  from this repository's `main` branch, and runs it. That script starts the
  container, builds the template, and copies the OS image into your output
  directory, owned by you. It stops before running anything if the download
  fails.
- **`ict`** runs `image-composer-tool` in the container with your current
  directory mounted at `/work`. Its arguments are the CLI's own.
- Both keep downloaded packages and work trees in the Docker volumes
  `ict-cache` and `ict-workspace`, so later builds reuse them. `ict serve`
  keeps its builds in `ict-webui-workspace`. Set `ICT_VOLUMES` to use
  another set of volumes; see
  [Choose, Pin, and Update the Image](#choose-pin-and-update-the-image).
- Both pass your proxy settings into the container. `ict` also passes
  `OPENAI_API_KEY`, `ICT_OEP_INSTALLER_TOKEN`, and
  `ICT_OEP_INSTALLER_TOKEN_FILE` when you set them. It uses the host's network,
  so `localhost` in the container is your host.

### Without the Functions

To run the build script once, pipe it into `bash`:

```bash
set -o pipefail
curl -fsSL https://raw.githubusercontent.com/open-edge-platform/image-composer-tool/main/scripts/build-in-container.sh \
  | bash -s -- --image "$ICT_IMAGE" my-image.yml ./out
```

Pull `ICT_IMAGE` first: the script does not pull, and for a missing image it
suggests building one from a checkout. Keep `set -o pipefail`: without it, a
failed download runs an empty script and reports success.

To read the script before you run it, download it with `curl -fsSLo
build-in-container.sh <URL>` and run it with `bash build-in-container.sh
--image "$ICT_IMAGE" ...`. To pin it, put a commit SHA in place of `main` in
the URL; the file at a commit never changes. Its options are documented in the
[CLI Quick Start (Docker)](./cli-docker-quick-start.md).

## 2. Write and Check a Template

A template describes the OS image. Only `image` and `target` are required; ICT
takes everything else from the defaults for the target, so this template
builds a minimal Ubuntu 24.04 RAW disk image:

```bash
mkdir -p ~/my-image
cd ~/my-image
cat > my-image.yml <<'EOF'
image:
  name: my-image       # output files are named <name>-<version>.<format>
  version: "24.04"
target:
  os: ubuntu
  dist: ubuntu24
  arch: x86_64
  imageType: raw
EOF
ict validate /work/my-image.yml
```

The [template reference](../architecture/image-composer-tool-templates.md)
describes every field. `ict validate --merged /work/my-image.yml` validates the
template merged with the OS defaults, and `ict resolve --full
/work/my-image.yml > merged.yml` saves that merged template, which is exactly
what a build uses.

## 3. Build the OS Image

```bash
cd ~/my-image
ict-build my-image.yml ./out
```

The build log streams to your terminal. When the build succeeds, the script
copies its outputs into `./out`:

```text
out/
└── Default_Raw/                                     # systemConfig.name, from the defaults
    ├── my-image-24.04.raw.gz                        # the disk image, gzip-compressed
    └── spdx_manifest_deb_my-image_<timestamp>.json  # the SBOM
```

`ict-build` takes `TEMPLATE` and `OUTPUT_DIR` as paths on your host. Paths in
the build log are inside the container, where the template's directory is
mounted read-only at `/input` and `OUTPUT_DIR` at `/output`.

Arguments after `--` go to `image-composer-tool build`; `ict build --help`
lists them:

```bash
ict-build my-image.yml ./out -- --hostname edge-01 --log-level debug
```

A flag that takes a file reads or writes it inside the container. A relative
path is resolved in the container's working directory, `/opt/ict`, so give
each file as an absolute container path:

| Flag | Give it | Example |
|------|---------|---------|
| `--ssh-authorized-key USER=FILE` | A file in the template's directory, at `/input/<file>` | `--ssh-authorized-key admin=/input/id_ed25519.pub` |
| `--oep-installer-token-file FILE` | As above; see [Private OEP Installer Files](#private-oep-installer-files) | `--oep-installer-token-file /input/oep-installer-token` |
| `--baseline-image FILE` | As above | `--baseline-image /input/baseline.raw` |
| `--config FILE` | As above; see [Configuration](#configuration) | `--config /input/ict.yml` |
| `--dotfile FILE` | A path under `/output`. The file is owned by root | `--dotfile /output/deps.dot` |
| `--log-file FILE` | A path under `/output`. The file is owned by root | `--log-file /output/build.log` |
| `--work-dir DIR`, `--cache-dir DIR` | Nothing: leave them unset so the build uses the volumes | |

The other flags take no file and work as they do outside a container:
`--hostname`, `--disk-strategy`, `--http-proxy`, `--https-proxy`,
`--ftp-proxy`, `--no-proxy`, `--workers`, `--no-cache`, `--inspect`,
`--system-packages-only`, `--log-level`, and `--verbose`. `--cve-check` is not
implemented yet and fails the build. To keep a log you own, save the
terminal's copy instead of using `--log-file`: `ict-build my-image.yml ./out
2>&1 | tee build.log`.

To keep a key or file elsewhere, copy it next to the template, or mount a
directory that holds everything with `--context`. An additional file
(`systemConfig.additionalFiles`) the build cannot reach does not fail it: the
log warns `Ignoring additional file entry with non-existent local path`, and
the image goes without the file. Options of the script go before `TEMPLATE`:

| Option | Effect |
|--------|--------|
| `--context DIR` | Mount `DIR` at `/input` instead of the template's directory. It must contain the template and every file the template or a flag references: an `extends` parent, additional files, scripts, keys |
| `--mount-point DIR` | Mount `OUTPUT_DIR` at `DIR` instead of `/output`. It must be an absolute path other than `/`, and not under `/input`, `/dev`, or `/opt/ict` |

## 4. Start From a Shipped Template

The image holds every template in this repository's `image-templates/`. To
list them:

```bash
docker run --rm --entrypoint find "$ICT_IMAGE" image-templates -name '*.yml'
```

Some of them reference files outside their own directory: an `extends`
parent, or an additional file under `image-templates/additionalfiles/` or
`config/`. Copy both directories out of the image into a directory you own,
then build from it with `--context`:

```bash
mkdir -p ~/ict-files
docker run --rm --entrypoint tar "$ICT_IMAGE" -C /opt/ict -c image-templates config \
  | tar -x -C ~/ict-files
cd ~/ict-files
ict-build --context . image-templates/ubuntu24/ubuntu24-x86_64-minimal-raw.yml ~/out
```

The copies are yours to edit. A template can only `extends` a template in its
own directory or below it, so to extend a shipped template, put your template
in the same directory as the parent, or copy the parent next to yours.

## 5. Run Other Commands

`ict` takes the same arguments as `image-composer-tool`. Inside the container:

- **Your files** are under `/work`, which is your current directory. A file
  outside it cannot be reached: `cd` to a directory that contains it first.
- **A relative path** names a file of the image, under `/opt/ict`, such as
  `image-templates/ubuntu24/ubuntu24-x86_64-minimal-raw.yml`.
- **Files `ict` writes** under `/work` are owned by root. You can delete them;
  to edit them, `sudo chown "$USER" <file>` first. Commands that print to
  standard output, such as `resolve`, do not have this problem when you
  redirect them on the host.
- **Builds** go through `ict-build`. `ict build` would leave the OS image in
  the `ict-workspace` volume, but `ict build --help` lists the build flags.

| Task | Command |
|------|---------|
| Show the version and commit of the image | `ict version` |
| List the commands, or the flags of one | `ict --help`, `ict build --help` |
| Validate a template | `ict validate /work/my-image.yml` |
| Validate it merged with the OS defaults | `ict validate --merged /work/my-image.yml` |
| Print the merged template, as a build uses it | `ict resolve --full /work/my-image.yml > merged.yml` |
| Print only the merged `extends` chain | `ict resolve /work/my-image.yml` |
| Inspect a RAW image | `ict inspect /work/out/Default_Raw/my-image-24.04.raw` |
| Compare two RAW images | `ict compare /work/a.raw /work/b.raw` |
| Delete cached packages and work trees | `ict cache clean --all` (add `--dry-run` to list them first) |
| Generate a template with AI | `ict ai "minimal ubuntu edge image with docker"` |
| Start the web UI | `ict serve` |

### Inspect and Compare Images

`inspect` and `compare` read RAW images, so decompress a `.raw.gz` first, with
`gunzip -k`. For example, from `~/my-image`:

```bash
gunzip -k out/Default_Raw/my-image-24.04.raw.gz
ict inspect /work/out/Default_Raw/my-image-24.04.raw
ict inspect --format json --pretty /work/out/Default_Raw/my-image-24.04.raw > report.json
ict inspect --extract-sbom=/work/sbom.json /work/out/Default_Raw/my-image-24.04.raw
```

Write `--extract-sbom=` with an equals sign and a path under `/work`. Its value
is optional, so `--extract-sbom /work/sbom.json` would leave the path out of
the flag, and without a value it writes into the container's working
directory, which disappears with the container. The
[Usage Guide](./usage-guide.md#comparing-overlay-outputs) shows `compare` with
`--mode=spdx`, `--hash-images`, and `--format json`; write every path there as
`/work/<path>`.

### Configuration

The image's configuration file is `/opt/ict/image-composer-tool.yml`. Its
relative paths resolve from `/opt/ict`, so `cache_dir` and `work_dir` are in
the volumes. To change a setting, copy the file out, edit it, and pass it with
`--config`:

```bash
docker run --rm --entrypoint cat "$ICT_IMAGE" /opt/ict/image-composer-tool.yml > ict.yml
ict --config /work/ict.yml validate --merged /work/my-image.yml
ict-build my-image.yml ./out -- --config /input/ict.yml
```

Keep `cache_dir`, `work_dir`, and `config_dir` as they are: `ict-build` copies
the OS image from `work_dir`, and the OS defaults are in `config_dir`.
`ict config init /work/ict.yml` also writes a configuration file, owned by
root.

### Generate Templates With AI

`ict ai` works as in
[AI-Powered Template Generation](./ai-template-generation.md):

- **Ollama** on your host works as it is: the container reaches
  `localhost:11434` on the host. Keep `localhost` in `no_proxy` if you use a
  proxy.
- **OpenAI**: `export OPENAI_API_KEY=...` first, then
  `ict ai --provider openai "..."`.
- **Save the result** with `--output /work/my-template.yml`. A bare name, such
  as `--output my-template`, saves it inside the container, where it is lost.

The search index is kept in the `ict-cache` volume, so it is built only once.

### Run the Web UI

```bash
ict serve
```

Then open <http://127.0.0.1:8080>. Because the container uses the host's
network, the UI listens on the host's `127.0.0.1` only, as `serve` does by
default. Do not pass `--host 0.0.0.0`: the UI has no authentication, and
anyone who can reach it can run builds as root on your host. `--port 9090`
picks another port.

Stop it with Ctrl+C: the server cancels a running build and waits for it to
unmount its filesystems. The build history and images stay in the
`ict-webui-workspace` volume for the next `ict serve`. Download an image from
the UI, or copy it out with the path the UI shows:

```bash
docker run --rm -v ict-webui-workspace:/opt/ict/webui-workspace:ro --entrypoint cat \
  "$ICT_IMAGE" /opt/ict/webui-workspace/builds/<build-id>/work/<path> > <image-file>
```

The [Web UI Quick Start](./web-ui-quick-start.md) describes the UI and that
volume's layout.

### Shell Completion

The completion script `ict completion bash` generates is for a command named
`image-composer-tool`. To complete `ict` with it, rename it:

```bash
source <(ict completion bash 2>/dev/null | sed 's/ image-composer-tool$/ ict/')
```

Each Tab press starts a container, so completion takes a moment.

## Choose, Pin, and Update the Image

- **Update** with `docker pull "$ICT_IMAGE"`. `ict` and `ict-build` use the
  image you have until you pull again, even when the tag has moved on.
- **Use another tag for one command**: put the variable in front of it, as in
  `ICT_IMAGE=gar-registry.caas.intel.com/image-composer-tool-saas/ict:latest-dev ict version`.
- **Pin an image** by its digest, which never changes:

  ```bash
  export ICT_IMAGE="$(docker image inspect --format '{{index .RepoDigests 0}}' "$ICT_IMAGE")"
  # e.g. gar-registry.caas.intel.com/image-composer-tool-saas/ict@sha256:...
  ```

- **Keep each tag's state apart.** Builds reuse what earlier builds left in the
  volumes. To keep one tag's leftovers out of another tag's builds, give each
  tag its own volumes with `ICT_VOLUMES`, which names the volumes
  `<ICT_VOLUMES>-cache`, `<ICT_VOLUMES>-workspace`, and
  `<ICT_VOLUMES>-webui-workspace`:

  ```bash
  ICT_IMAGE=gar-registry.caas.intel.com/image-composer-tool-saas/ict:latest-dev \
  ICT_VOLUMES=ict-dev ict-build my-image.yml ./out-dev
  ```

- **Run two builds at once** only with different `ICT_VOLUMES`. The script
  copies every file a build wrote to the work tree after it started, so two
  builds in one workspace volume can receive each other's images.

## Behind a Proxy

Export your proxy settings before you run the commands:

```bash
export http_proxy=http://proxy.example.com:911
export https_proxy=http://proxy.example.com:912
export no_proxy=localhost,127.0.0.1
```

`curl` reads them to download the script, and both functions pass them, and
their uppercase forms, into the container for the build's downloads. `docker
pull` does not read them: the Docker daemon pulls the image, with the proxy in
its own configuration.

The proxy flags of `build`, such as `--http-proxy`, are a different thing:
they set the proxy of the OS image you build, not the proxy of the build.

## Private OEP Installer Files

Templates with a `systemConfig.oepInstaller` section download the OEP
Installer from GitHub, and need a token with read access when those files are
in a private repository.

- **For `ict-build`**, save the token next to the template and pass it with
  `--oep-installer-token-file`. Keep it out of version control.

  ```bash
  (umask 077; gh auth token > ~/my-image/oep-installer-token)
  cd ~/my-image
  ict-build my-image.yml ./out -- --oep-installer-token-file /input/oep-installer-token
  ```

- **For `ict serve`**, give the server a token for every build, from a file in
  the current directory. Without one, users can enter a token for each build
  in the UI.

  ```bash
  ICT_OEP_INSTALLER_TOKEN_FILE=/work/oep-installer-token ict serve
  ```

## Stop and Clean Up

- **Stop a build** with Ctrl+C. ICT unmounts its filesystems and detaches its
  loop devices, and nothing is copied. Do not `docker kill` the container: a
  build killed without that cleanup can leave mounts and loop devices behind
  on the host.
- **Reclaim disk space** taken by downloaded packages, work trees, and the web
  UI's builds with
  `docker volume rm ict-cache ict-workspace ict-webui-workspace`, and the
  image's with `docker image rm "$ICT_IMAGE"`.

## Troubleshooting

| Symptom | Fix |
|---------|-----|
| `docker pull` fails with `no such host`, a timeout, or `manifest unknown` | The registry is reachable from the Intel network only; check the tag's spelling. Behind a proxy, configure the Docker daemon's proxy, as in [Behind a Proxy](#behind-a-proxy). |
| `curl: (6) Could not resolve host` or `curl: (22)` from `ict-build` | It could not download the script. Export your proxy settings, or check the URL. |
| `image '...' not found; build it with ...` | The script does not pull. Run `docker pull "$ICT_IMAGE"`, or use `ict-build`, which pulls. |
| `no such file or directory` for a path given to `ict` | Give it as `/work/<path>`, from a current directory that contains the file. |
| A file the template references is not found, or the log warns `Ignoring additional file entry with non-existent local path` | It is outside the mounted directory. Pass a `--context` that contains it. |
| `extends path escapes child template's directory` | Move your template next to its parent, as in [Start From a Shipped Template](#4-start-from-a-shipped-template). |
| `connection refused` for `localhost:11434` from `ict ai` | Ollama is not running on your host. Start it, or use `--provider openai`. |
| `address already in use` from `ict serve` | Another service uses port 8080. Pass `--port 9090`. |
| `Permission denied` editing a file `ict` wrote | It is owned by root. Run `sudo chown "$USER" <file>`. |

## Next Steps

- [CLI Quick Start (Docker)](./cli-docker-quick-start.md) builds the ICT
  container image from a checkout and describes the build script in full.
- [Usage Guide](./usage-guide.md) covers each command; write file paths as
  `/work/<path>` when you run them with `ict`.
- [CLI Specification](../architecture/image-composer-tool-cli-specification.md)
  documents every command and flag.
