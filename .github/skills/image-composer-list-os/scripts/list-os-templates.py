#!/usr/bin/env python3
"""List available OS build targets from image-templates YAML files."""

from __future__ import annotations

import argparse
from collections import Counter
from pathlib import Path
import sys

import yaml


def find_templates_dir() -> Path | None:
    """Find image-templates directory starting from cwd upwards."""
    cwd = Path.cwd().resolve()
    for parent in [cwd] + list(cwd.parents):
        candidate = parent / "image-templates"
        if candidate.is_dir() and any(candidate.glob("*.yml")):
            return candidate
    return None


def parse_template(path: Path) -> dict:
    """Parse one template and extract canonical fields for display."""
    with path.open("r", encoding="utf-8") as file_obj:
        data = yaml.safe_load(file_obj) or {}

    target = data.get("target") if isinstance(data.get("target"), dict) else {}
    metadata = data.get("metadata") if isinstance(data.get("metadata"), dict) else {}

    description = metadata.get("description")
    if not description:
        system_config = data.get("systemConfig")
        if isinstance(system_config, dict):
            description = system_config.get("description")

    return {
        "template": path.name,
        "os": target.get("os", "unknown"),
        "dist": target.get("dist", "unknown"),
        "arch": target.get("arch", "unknown"),
        "image_type": target.get("imageType", "unknown"),
        "description": description or "(no description)",
    }


def matches(record: dict, args: argparse.Namespace) -> bool:
    """Return True when a record matches all requested filters."""
    if args.os and str(record["os"]).lower() != args.os.lower():
        return False
    if args.dist and str(record["dist"]).lower() != args.dist.lower():
        return False
    if args.arch and str(record["arch"]).lower() != args.arch.lower():
        return False
    if args.image_type and str(record["image_type"]).lower() != args.image_type.lower():
        return False

    if args.keyword:
        combined = " ".join(
            [
                str(record["template"]),
                str(record["os"]),
                str(record["dist"]),
                str(record["arch"]),
                str(record["image_type"]),
                str(record["description"]),
            ]
        ).lower()
        if args.keyword.lower() not in combined:
            return False

    return True


def print_summary(records: list[dict], templates_dir: Path) -> None:
    """Print summary counts by OS family."""
    os_counter = Counter(r["os"] for r in records)
    dist_counter = Counter(r["dist"] for r in records)
    arch_counter = Counter(r["arch"] for r in records)
    type_counter = Counter(r["image_type"] for r in records)

    print(f"Templates directory: {templates_dir}")
    print(f"Total templates: {len(records)}")
    print()

    print("OS families:")
    for os_name, count in sorted(os_counter.items(), key=lambda item: (str(item[0]), item[1])):
        print(f"  - {os_name}: {count}")
    print()

    print("Distributions:")
    for dist_name, count in sorted(dist_counter.items(), key=lambda item: (str(item[0]), item[1])):
        print(f"  - {dist_name}: {count}")
    print()

    print("Architectures:")
    for arch_name, count in sorted(arch_counter.items(), key=lambda item: (str(item[0]), item[1])):
        print(f"  - {arch_name}: {count}")
    print()

    print("Image types:")
    for image_type, count in sorted(type_counter.items(), key=lambda item: (str(item[0]), item[1])):
        print(f"  - {image_type}: {count}")
    print()


def print_details(records: list[dict]) -> None:
    """Print detailed per-template listing."""
    if not records:
        print("No templates match the requested filters.")
        return

    print("Template details:")
    for record in sorted(records, key=lambda r: r["template"]):
        print(
            f"  - {record['template']}: "
            f"os={record['os']}, dist={record['dist']}, arch={record['arch']}, imageType={record['image_type']}"
        )
        print(f"    desc: {record['description']}")


def build_arg_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="List available OS build targets from image-templates/*.yml"
    )
    parser.add_argument("--os", help="Filter by target.os (e.g. ubuntu, debian, azl, elxr)")
    parser.add_argument("--dist", help="Filter by target.dist (e.g. ubuntu24, debian13)")
    parser.add_argument("--arch", help="Filter by target.arch (e.g. x86_64, aarch64)")
    parser.add_argument("--image-type", help="Filter by target.imageType (e.g. raw, iso, img)")
    parser.add_argument("--keyword", help="Case-insensitive keyword search across all fields")
    parser.add_argument(
        "--summary-only",
        action="store_true",
        help="Show summary counts only, without detailed template listing",
    )
    return parser


def main() -> int:
    parser = build_arg_parser()
    args = parser.parse_args()

    templates_dir = find_templates_dir()
    if templates_dir is None:
        print("ERROR: Could not find image-templates/ with any .yml files.")
        print("Run this command from the project root or inside the repository.")
        return 1

    records: list[dict] = []
    parse_errors = 0

    for path in sorted(templates_dir.glob("*.yml")):
        try:
            records.append(parse_template(path))
        except Exception as err:  # pylint: disable=broad-except
            parse_errors += 1
            print(f"WARNING: failed to parse {path.name}: {err}", file=sys.stderr)

    filtered = [record for record in records if matches(record, args)]

    print_summary(filtered, templates_dir)
    if not args.summary_only:
        print_details(filtered)

    if parse_errors:
        print()
        print(f"Parse warnings: {parse_errors}")

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
