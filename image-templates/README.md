# Image templates

Every file here is a working example you can build, copy, or inherit from. A
template is the YAML description of one image: which distribution, which
packages, which disk layout.

```bash
# build one
sudo -E ./image-composer-tool build image-templates/ubuntu24/ubuntu24-x86_64-minimal-raw.yml

# check it without building
./image-composer-tool validate image-templates/ubuntu24/ubuntu24-x86_64-minimal-raw.yml
```

## Layout

Templates are grouped by the distribution they target — the value of
`target.dist` inside the file:

| Directory | Distribution | Templates | CI-built |
|---|---|---|---|
| [`ubuntu24/`](./ubuntu24/) | Ubuntu 24.04 | 25 | 6 |
| [`debian13/`](./debian13/) | Debian 13 | 7 | 0 |
| [`elxr12/`](./elxr12/) | Wind River eLxr 12 | 7 | 5 |
| [`emt3/`](./emt3/) | Edge Microvisor Toolkit 3 | 7 | 4 |
| [`azl3/`](./azl3/) | Azure Linux 3 | 6 | 5 |
| [`elxr13/`](./elxr13/) | Wind River eLxr 13 (26.04) | 5 | 4 |
| [`el10/`](./el10/) | Red Hat compatible 10 | 2 | 0 |
| [`ubuntu26/`](./ubuntu26/) | Ubuntu 26.04 | 1 | 0 |
| | **Total** | **60** | **24** |

Two directories hold data rather than templates: `additionalfiles/` (files
templates copy into the image) and `elxr-cloud-amd64/` (the payload for the
eLxr cloud template).

**Why by distribution?** Because it is the one grouping that never gets in the
way of inheritance. A template can only inherit from a parent in its own
directory, and every level of a chain must target the same OS, distribution,
architecture and image type. Grouping by distribution can therefore never
separate two templates that were allowed to form a chain — whereas grouping by
purpose (`minimal/`, `robotics/`, …) would, because a chain legitimately runs
from minimal to robotics. See [COMPOSITION.md](./COMPOSITION.md).

Filenames still carry the full `<dist>-<arch>-<purpose>-<imageType>.yml`
convention, so a name remains unambiguous when it appears on its own — in a
build script, a bug report, or `internal/api/data/manifest.yaml`. See
[CONVENTIONS.md](./CONVENTIONS.md).

## Choosing a starting point

| If you want… | Start from |
|---|---|
| the smallest bootable image for a distribution | `<dist>/<dist>-<arch>-minimal-raw.yml` |
| an edge node with agents, containers and an immutable root | `<dist>/<dist>-<arch>-edge-raw.yml` |
| an installer you can boot from USB | any `*-iso.yml` |
| a cloud image | `ubuntu24/ubuntu24-server-cloud-amd64.yml`, `elxr12/elxr-cloud-amd64.yml` |
| AI video analytics | any `*-dlstreamer.yml` |
| a robotics stack | `ubuntu24/ubuntu24-x86_64-robotics-jazzy-raw.yml` |
| to add features to an image that already works | [COMPOSITION.md](./COMPOSITION.md) |

Pick the template whose `target` matches your hardware, copy it, and edit. If
your change is *additive* — a few more packages, one more config command — you
probably want `extends:` instead of a copy.

## Composed templates

Five templates inherit from another instead of restating it:

| Template | Inherits | Adds |
|---|---|---|
| `emt3/emt3-x86_64-emf-raw.yml` | `emt3-x86_64-edge-raw.yml` | persistent `/opt`, verity root, 2 packages |
| `emt3/emt3-x86_64-emf-rt-raw.yml` | `emt3-x86_64-emf-raw.yml` | real-time GPU driver |
| `emt3/emt3-x86_64-dlstreamer.yml` | `emt3-x86_64-edge-raw.yml` | media/AI stack, 3 repositories, 8GiB disk |
| `debian13/debian13-x86_64-bb-raw.yml` | `debian13-x86_64-minimal-raw.yml` | `initramfs-tools`, 3 hook files, 3 commands |
| `ubuntu24/ubuntu24-x86_64-extends-example-raw.yml` | `ubuntu24-x86_64-minimal-raw.yml` | two packages (minimal demo) |

`ubuntu24/ubuntu24-x86_64-overlay-raw.yml` demonstrates the other composition
mode: layering packages onto a pre-built image instead of building from scratch.

To see what any of them actually resolves to:

```bash
./image-composer-tool resolve <template>          # the chain, without OS defaults
./image-composer-tool resolve <template> --full   # exactly what gets built
```

## What CI builds

24 of the 60 templates are built by a `scripts/build_*.sh` script on every pull
request. The rest are validated by schema but **never built by CI**, so if you
change one, build it yourself before opening a PR. Each per-distribution README
marks which of its templates are covered.

## Further reading

- [COMPOSITION.md](./COMPOSITION.md) — `extends:` and overlay mode, the rules
  they enforce, and the four traps worth knowing before you split a template.
- [CONVENTIONS.md](./CONVENTIONS.md) — naming, key order, and how to add a
  template or a new distribution directory.
- [Template reference](../docs/user-guide/architecture/image-composer-tool-templates.md)
  — every field, and the full merge table.
