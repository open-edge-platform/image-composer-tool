# Web UI Quick Start (Docker Compose)

This guide gets the ICT web UI running on your machine with Docker alone, then
walks you through composing your first image from the browser. You do not need
Go, Node.js, the image-build toolchain, or sudoers rules: everything runs in one
container built from your checkout.

> **Warning: the container is privileged, and therefore root-equivalent on the
> host.** Image builds create loop devices, mount filesystems, and chroot, so the
> container runs with `privileged: true` and the host's `/dev` mounted. The web UI
> has no authentication of its own, so anyone who can reach it can run builds as
> root on your machine. The port is published on `127.0.0.1` only; keep it that
> way, and run the container only on a machine where you would give the UI's
> users root.

## Prerequisites

- A Linux x86_64 host with Docker Engine and the Docker Compose plugin. Check
  with `docker compose version`.
- At least 15 GB of free disk space. The image and its build cache take about
  5 GB, and a minimal RAW build needs about 5 GB more while it runs.
- Network access to the package repositories your images use. Behind a proxy,
  see [Behind a Proxy](#behind-a-proxy) first.

## 1. Start the Web UI

```bash
git clone https://github.com/open-edge-platform/image-composer-tool.git
cd image-composer-tool
docker compose up --build
```

The first run builds the ICT image (the web UI, the `image-composer-tool` and
`live-installer` binaries, and the Ubuntu 24.04 build toolchain), which takes
several minutes. The UI is ready when the log shows:

```text
ICT web UI API listening on 0.0.0.0:8080
```

To keep your terminal, start it in the background instead and follow the log
separately:

```bash
docker compose up --build -d
docker compose logs -f
```

## 2. Open the UI

Browse to <http://127.0.0.1:8080>. The port is reachable from this host only.
If Docker runs on a remote machine, forward the port over SSH and browse to the
same address on your own machine:

```bash
ssh -L 8080:127.0.0.1:8080 <user>@<docker-host>
```

## 3. Compose an Image

1. On the **Basic** tab, choose a **Use Case**, **SKU**, **Platform**, and
   **Operating System**.
2. Select **Compose Image**. In the dialog that opens, enter a password or SSH
   public key for any account that needs one, and confirm.
3. The **Compose Image** tab streams the build log. When the status reads
   **Completed**, download the image, its SBOM, and the merged template from the
   **Artefacts** table.

Every compose is listed in the **History** sidebar, where you can reopen it,
including after a restart.

## 4. Stop and Restart

```bash
# Stop the UI (Ctrl+C does the same when it runs in the foreground)
docker compose down

# Start it again; history and artifacts are kept
docker compose up
```

Stopping cancels a running build and gives it 60 seconds to unmount its
filesystems and detach its loop devices, so let `docker compose down` finish
rather than killing the container.

After you pull new commits, add `--build` so the image picks them up:

```bash
git pull
docker compose up --build
```

To remove the UI's data as well, which deletes every build's history and
artifacts:

```bash
docker compose down -v
```

## Behind a Proxy

Export your proxy settings before starting the UI. Compose passes them to the
image build and to the running server, which forwards them to every build:

```bash
export http_proxy=http://proxy.example.com:911
export https_proxy=http://proxy.example.com:912
export no_proxy=localhost,127.0.0.1
docker compose up --build
```

The uppercase forms (`HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY`) are passed too.
Set both forms if your tools disagree about which one they read.

## Troubleshooting

| Symptom | Fix |
|---------|-----|
| `port is already allocated` or `address already in use` on start | Another service uses port 8080. Stop it, or change the host side of the `ports` entry in `docker-compose.yml`, keeping the `127.0.0.1:` prefix, for example `"127.0.0.1:9090:8080"`. |
| The image build fails downloading npm, Go, or apt packages | Set your proxy variables as shown in [Behind a Proxy](#behind-a-proxy), then run `docker compose up --build` again. |
| A compose fails while resolving or downloading packages | Check the build log in the UI for the repository it could not reach; behind a proxy, check `no_proxy` does not send that host direct. |
| The UI still shows old behaviour after `git pull` | Run `docker compose up --build`, then hard-refresh the browser (Ctrl+Shift+R). |

## Next Steps

- [Running the Web UI in a Container](./usage-guide.md#running-the-web-ui-in-a-container)
  covers what the volumes hold, running the CLI inside the same container, and
  building the image with Earthly.
- [Serve Command](../architecture/image-composer-tool-cli-specification.md#serve-command)
  documents every `serve` flag.
- [Quick Start](./quick-start.md) builds an image with the CLI instead of the
  web UI.
