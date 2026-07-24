# Quick Start

This guide walks you through building your first image in three steps. For full build options (Earthly, Debian package) and prerequisite details, see the
[Installation Guide](./installation.md). To check general prerequisites, go to [Prerequisites](./prerequisites.md), and for the practical guide on common ICT workflows, refer to [Usage Guide](./usage-guide.md).

## Build and Run

1. Clone and build (requires Go 1.24+)

```bash
git clone https://github.com/open-edge-platform/image-composer-tool.git
cd image-composer-tool
go build -buildmode=pie -ldflags "-s -w" ./cmd/image-composer-tool
```

   Alternatively, run it directly without building a binary:

   ```bash
   go run ./cmd/image-composer-tool --help
   ```

1. Install the required system tools:

```bash
sudo apt install systemd-ukify mmdebstrap
```

1. Compose an image using one of the templates:

```bash
sudo -E ./image-composer-tool build image-templates/azl3-x86_64-edge-raw.yml
```
