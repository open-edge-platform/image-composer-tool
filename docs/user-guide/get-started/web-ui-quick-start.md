# Web UI Quick Start (Docker Compose)

Image Composer Tool (ICT) builds custom Linux OS images, such as bootable RAW
disk images and ISO installers, from YAML templates. This guide runs ICT's web
UI in a Docker container and walks you through building your first OS image
from the browser.

The host needs only Docker. Everything else comes in the ICT container image,
which Docker builds from your checkout of this repository: the
`image-composer-tool` binary, the web UI, and the programs a build runs, such
as `debootstrap`, `mkfs`, and `grub`. You do not install Go, Node.js, or any
of those programs, and you do not set up sudo rules.

> **Warning: the container is privileged, and therefore root-equivalent on the
> host.** Building an OS image creates loop devices, mounts filesystems, and
> chroots into them, so the container runs with `privileged: true` and the
> host's `/dev` mounted. The web UI has no authentication of its own, so anyone
> who can reach it can run builds as root on your machine. The port is
> published on `127.0.0.1` only, so only this host can reach it. Keep it that
> way, and run the container only on a machine where you would trust everyone
> who can reach the UI with root.

## Prerequisites

- A Linux x86_64 host with Git, Docker Engine, and the Docker Compose plugin.
  Check with `docker compose version`. Your user must be allowed to run
  `docker`: be in the `docker` group, or put `sudo` in front of each `docker`
  command below.
- At least 15 GB of free disk space. The container image and Docker's build
  cache take about 5 GB, and a minimal RAW image needs about 5 GB more while it
  builds. Larger images, such as ISO installers, need more.
- Network access to the package repositories your images download from. Behind
  a proxy, read [Behind a Proxy](#behind-a-proxy) first.

## 1. Start the Web UI

```bash
git clone https://github.com/open-edge-platform/image-composer-tool.git
cd image-composer-tool
docker compose up --build
```

`docker compose` reads `docker-compose.yml` at the root of the checkout. The
first run builds the ICT container image, which takes several minutes; later
runs reuse Docker's cache. The UI is ready when the log shows:

```text
ICT web UI API listening on 0.0.0.0:8080
```

`0.0.0.0` means every network interface inside the container, which Docker
needs to forward the port. On the host, the port is still reachable from
`127.0.0.1` only.

To get your terminal back, start it in the background instead and follow the
log separately:

```bash
docker compose up --build -d
docker compose logs -f
```

## 2. Open the UI

Browse to <http://127.0.0.1:8080>. If Docker runs on another machine, forward
the port over SSH, then browse to the same address on your own machine:

```bash
ssh -L 8080:127.0.0.1:8080 <user>@<docker-host>
```

## 3. Build an OS Image

1. On the **Basic** tab, choose a **Use Case**, **SKU**, **Platform**, and
   **Operating System**. Together they select a ready-made template.
2. Select **Compose Image**. In the dialog that opens, enter a password or SSH
   public key for any account that needs one, and confirm.
3. The **Compose Image** tab streams the build log. When the status reads
   **Completed**, the **Artefacts** table lists what the build produced:
   download the OS image, its software bill of materials (SBOM), and the
   template the build used, with every inherited setting filled in. To reach
   the files another way, see
   [Where Your Files and Image Artifacts Are](#where-your-files-and-image-artifacts-are).

The **History** sidebar lists every build, and you can reopen any of them,
including after a restart.

## 4. Stop and Restart

```bash
# Stop the UI (Ctrl+C does the same when it runs in the foreground)
docker compose down

# Start it again; build history and images are kept
docker compose up
```

Stopping cancels a running build and gives it 60 seconds to unmount its
filesystems and detach its loop devices. Let `docker compose down` finish
rather than killing the container, or those mounts and loop devices stay
behind on the host.

After you pull new commits, add `--build` so the container image picks them
up:

```bash
git pull
docker compose up --build
```

To delete the UI's data as well, which removes every build's history and
images:

```bash
docker compose down -v
```

## Where Your Files and Image Artifacts Are

Builds run inside the container, so every path the UI shows, such as the
**Path** column of the **Artefacts** table, is a path inside the container,
not on your host:

```text
/opt/ict/webui-workspace/builds/<build-id>/work/<os>-<dist>-<arch>/imagebuild/<config>/<image-file>
```

- `<build-id>` is the unique ID the UI gives each build.
- `<os>-<dist>-<arch>` comes from the template's `target` section, for example
  `ubuntu-ubuntu24-x86_64`.
- `<config>` is the template's `systemConfig.name`, for example `minimal`.

`/opt/ict/webui-workspace` is a Docker named volume called `webui-workspace`.
Docker stores it in its own data directory, not in your checkout, and keeps it
when the container stops. Each build has its own directory under `builds/`:

| Path under `builds/<build-id>/` | Contents |
|---------------------------------|----------|
| `work/<os>-<dist>-<arch>/imagebuild/<config>/` | The OS image (for example `.iso`, `.raw.gz`, or `.vhdx`) and its SBOM (`spdx_manifest_*.json`). ISO builds add a `.composition.json` manifest and `template-dump.yaml` here, and put the installer's initial RAM disk (`.img`) in a second directory beside this one |
| `template.yml` | The template the build used, with every inherited setting filled in |
| `compose.log` | The build log |
| `meta.json` | The build's entry in **History** |
| `cache/` | The packages the build downloaded |

You can get a file out in three ways:

- **Download it from the UI.** Use the download icon in the build's
  **Artefacts** table. If Docker runs on another machine, forward the port
  first, as described in [Open the UI](#2-open-the-ui).
- **Copy it out with Compose.** Run this from your checkout, with the path the
  UI shows:

  ```bash
  docker compose cp ict:/opt/ict/webui-workspace/builds/<build-id>/work/<os>-<dist>-<arch>/imagebuild/<config>/<image-file> .
  ```

- **Read it on the host.** Docker keeps the volume under its own data
  directory, which only root can read. Compose puts your project's name, by
  default the checkout's directory name, in front of the volume's name, so
  look the volume up by its label:

  ```bash
  docker volume inspect --format '{{ .Mountpoint }}' \
    "$(docker volume ls -q --filter label=com.docker.compose.volume=webui-workspace)"
  # e.g. /var/lib/docker/volumes/image-composer-tool_webui-workspace/_data

  sudo ls /var/lib/docker/volumes/image-composer-tool_webui-workspace/_data/builds/
  ```

  Files you copy from there with `sudo cp` are owned by root, so `chown` them
  to your user afterwards.

`docker compose down -v` deletes the volume and every file in it, so copy out
anything you want to keep first.

## Behind a Proxy

Export your proxy settings before you start the UI. Compose passes them to the
container image build and to the running server, which passes them on to
every build:

```bash
export http_proxy=http://proxy.example.com:911
export https_proxy=http://proxy.example.com:912
export no_proxy=localhost,127.0.0.1
docker compose up --build
```

The uppercase forms (`HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY`) are passed too.
Set both forms if your tools disagree about which one they read. Docker does
not store any of them in the container image.

## Troubleshooting

| Symptom | Fix |
|---------|-----|
| `permission denied while trying to connect to the Docker daemon socket` | Your user may not run `docker`. Add it to the `docker` group and log in again, or put `sudo` in front of the command. |
| `port is already allocated` or `address already in use` on start | Another service uses port 8080. Stop it, or change the host port in the `ports` entry of `docker-compose.yml`, keeping the `127.0.0.1:` prefix, for example `"127.0.0.1:9090:8080"`. |
| Building the container image fails while downloading npm, Go, or apt packages | Set your proxy variables as shown in [Behind a Proxy](#behind-a-proxy), then run `docker compose up --build` again. |
| A build in the UI fails while resolving or downloading packages | The build log in the UI names the repository it could not reach. Behind a proxy, check that `no_proxy` does not send that host around the proxy. |
| The UI still behaves as before after `git pull` | Run `docker compose up --build`, then hard-refresh the browser (Ctrl+Shift+R). |

## Next Steps

- [Running the Web UI With Docker Compose](./usage-guide.md#running-the-web-ui-with-docker-compose)
  explains each setting in `docker-compose.yml` and how to run other CLI
  commands in the container.
- [Serve Command](../architecture/image-composer-tool-cli-specification.md#serve-command)
  documents every flag of `serve`, the command the container runs.
- [Quick Start](./quick-start.md) builds an image with the CLI installed on the
  host, without Docker.
