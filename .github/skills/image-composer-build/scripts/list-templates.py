#!/usr/bin/env python3
"""
List all image-composer templates with descriptions, grouped by OS family.
Optionally filter by keyword: python list-templates.py <keyword>

Usage:
    python list-templates.py             # show all 63 templates
    python list-templates.py ros2        # show only ROS2-related templates
    python list-templates.py cloud       # show cloud images
    python list-templates.py desktop     # show desktop/ISO variants
"""

import sys
import yaml
from pathlib import Path

TEMPLATES_DIR = Path(__file__).resolve().parent.parent.parent.parent.parent / "image-templates"
# Actually resolve relative to cwd or a known path — let's check a few common places
CANDIDATE_DIRS = [
    TEMPLATES_DIR,
    Path.cwd() / "image-templates",
    Path.cwd().parent / "image-templates",
]

def find_templates_dir():
    for d in CANDIDATE_DIRS:
        if d.exists() and list(d.glob("*.yml")):
            return d
    # Search from cwd upward
    cwd = Path.cwd()
    for parent in [cwd] + list(cwd.parents):
        td = parent / "image-templates"
        if td.exists() and list(td.glob("*.yml")):
            return td
    return None


# OS family grouping rules — matched against the filename prefix
OS_FAMILIES = [
    ("Ubuntu 24.04",        "ubuntu24-",         0),
    ("Ubuntu 26.04",        "ubuntu26-",         1),
    ("Ubuntu Minimal Cloud","ubuntu-minimal-",   2),
    ("Debian 13",           "debian13-",         3),
    ("AZL3 (Azure Linux 3)","azl3-",            4),
    ("ELXR12",              "elxr12-",           5),
    ("ELXR Edge 26.04",     "elxr-edge-26.04-",  6),
    ("EMT3",                "emt3-",             7),
    ("RCD10 (Rocky Linux)", "rcd10-",            8),
    ("Other / Custom",      "",                  9),  # catch-all
]


def classify_os(filename):
    """Return (family_name, sort_key) for a template filename."""
    for name, prefix, sort_key in OS_FAMILIES:
        if prefix and filename.startswith(prefix):
            return name, sort_key
    return "Other / Custom", 9


def extract_description(filepath):
    """Extract a human-readable description from a template YAML file."""
    try:
        with open(filepath) as f:
            data = yaml.safe_load(f)
    except Exception:
        return None

    if not data:
        return None

    # Priority 1: metadata.description
    metadata = data.get("metadata")
    if isinstance(metadata, dict):
        desc = metadata.get("description")
        if desc and isinstance(desc, str) and len(desc) > 5:
            return desc.strip()

    # Priority 2: systemConfig.description (some templates use this)
    syscfg = data.get("systemConfig")
    if isinstance(syscfg, dict):
        desc = syscfg.get("description")
        if desc and isinstance(desc, str) and len(desc) > 5:
            return desc.strip()

    # Priority 3: image.description
    img = data.get("image")
    if isinstance(img, dict):
        desc = img.get("description")
        if desc and isinstance(desc, str) and len(desc) > 5:
            return desc.strip()

    return None


def format_size(num_bytes):
    """Format byte count to human-readable size (if available)."""
    if not num_bytes:
        return ""
    for unit in ("B", "KB", "MB", "GB", "TB"):
        if num_bytes < 1024:
            return f"{num_bytes:.0f} {unit}"
        num_bytes /= 1024
    return f"{num_bytes:.0f} TB"


def get_image_type(filepath):
    """Extract imageType from target section."""
    try:
        with open(filepath) as f:
            data = yaml.safe_load(f)
    except Exception:
        return None
    if not data:
        return None
    target = data.get("target")
    if isinstance(target, dict):
        return target.get("imageType")
    return None


def main():
    templates_dir = find_templates_dir()
    if templates_dir is None:
        print("ERROR: Could not find image-templates/ directory.")
        print("Run this script from the project root (where image-templates/ lives).")
        sys.exit(1)

    # Gather all template files
    files = sorted(templates_dir.glob("*.yml"))

    # Parse filter keyword
    filter_keyword = None
    if len(sys.argv) > 1:
        filter_keyword = sys.argv[1].lower().strip()

    # Build entries: (filename, family, sort_key, description, image_type)
    entries = []
    for fp in files:
        fname = fp.name
        family, sort_key = classify_os(fname)
        desc = extract_description(fp)
        itype = get_image_type(fp)
        entry_text = fname

        # Build the display text for filtering
        display_text = f"{fname} {desc or ''} {family} {itype or ''}"

        if filter_keyword and filter_keyword not in display_text.lower():
            continue

        if desc:
            line = f"  {fname:<55s} {desc}"
        else:
            line = f"  {fname:<55s} (no description)"

        if itype:
            line += f"  [{itype}]"

        entries.append((family, sort_key, line))

    if not entries:
        if filter_keyword:
            print(f'No templates match "{filter_keyword}".')
        else:
            print("No templates found.")
        sys.exit(0)

    # Group by family and print
    current_family = None
    count = 0
    for family, sort_key, line in entries:
        if family != current_family:
            if current_family is not None:
                print()
            print(f"━━━ {family} ━━━")
            current_family = family
        print(line)
        count += 1

    print(f"\n{'='*60}")
    print(f"{count} template{'s' if count != 1 else ''} found in {templates_dir}")

    # If a filter was used, show the build hint
    if filter_keyword:
        # Show exact matches first
        exact_matches = [e[2].split()[0].strip() for e in entries if e[2].split()[0].strip()]
        if exact_matches:
            print()
            print("To build:")
            for name in exact_matches:
                print(f"  sudo -E ./image-composer-tool build image-templates/{name}")


if __name__ == "__main__":
    main()
