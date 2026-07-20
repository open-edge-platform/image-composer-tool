#!/usr/bin/env python3
"""
Customize an image-composer template with extra packages and repos,
saving the result to user-templates/ so the canonical
image-templates/ directory stays pristine.

Usage:
  python3 customize-template.py <base_template> --name <output_name> [options]

Examples:
  # Add ROS2 to minimal-raw
  python3 customize-template.py ubuntu24-x86_64-minimal-raw.yml \\
      --name ubuntu24-ros2-custom \\
      --add-packages "ros-jazzy-ros-base,ros-jazzy-demo-nodes-py" \\
      --add-repo "http://packages.ros.org/ros2/ubuntu noble main" \\
      --add-repo-key "https://raw.githubusercontent.com/ros/rosdistro/master/ros.key"

  # Add vim + git to dlstreamer image
  python3 customize-template.py ubuntu24-x86_64-dlstreamer.yml \\
      --name my-dlstreamer \\
      --add-packages "vim,git,htop" \\
      --build

  # List available base templates
  python3 customize-template.py --list-base

  # List existing user templates
  python3 customize-template.py --list
"""

import os
import sys
import argparse
import subprocess
import yaml
from pathlib import Path


# --- Paths ---
PROJECT_ROOT = Path(__file__).resolve().parents[4]
DEFAULT_USER_TEMPLATES_DIR = PROJECT_ROOT / "user-templates"
USER_TEMPLATES_DIR = Path(
    os.environ.get("IMAGE_COMPOSER_USER_TEMPLATES_DIR", str(DEFAULT_USER_TEMPLATES_DIR))
)


def find_base_dir():
    """Find the image-templates/ directory (from cwd or parent dirs)."""
    cwd = Path.cwd()
    for parent in [cwd] + list(cwd.parents):
        td = parent / "image-templates"
        if td.exists() and list(td.glob("*.yml")):
            return td
    return None


def clean_name(s):
    """Sanitize a name to be filesystem-safe."""
    return "".join(c if c.isalnum() or c in "-_." else "_" for c in s)


def list_base_templates():
    """Print available base templates grouped by OS family."""
    base_dir = find_base_dir()
    if not base_dir:
        print("ERROR: No image-templates/ directory found. Run from the project root.")
        return

    files = sorted(base_dir.glob("*.yml"))
    print(f"Base templates in {base_dir}:\n")
    for f in files:
        name = f.name
        desc = ""
        try:
            with open(f) as fh:
                data = yaml.safe_load(fh)
            if data:
                meta = data.get("metadata", {})
                if isinstance(meta, dict) and meta.get("description"):
                    desc = meta["description"]
                elif data.get("systemConfig", {}).get("description"):
                    desc = data["systemConfig"]["description"]
        except Exception:
            pass
        if desc:
            print(f"  {name:<55s} {desc}")
        else:
            print(f"  {name}")
    print(f"\n  {len(files)} templates total")


def list_user_templates():
    """Print existing user templates."""
    USER_TEMPLATES_DIR.mkdir(parents=True, exist_ok=True)
    files = sorted(USER_TEMPLATES_DIR.glob("*.yml"))
    if not files:
        print(f"No user templates in {USER_TEMPLATES_DIR}")
        return

    print(f"User templates in {USER_TEMPLATES_DIR}:\n")
    for f in files:
        desc = ""
        try:
            with open(f) as fh:
                data = yaml.safe_load(fh)
            if data:
                meta = data.get("metadata", {})
                if isinstance(meta, dict) and meta.get("description"):
                    desc = meta["description"]
                elif data.get("systemConfig", {}).get("description"):
                    desc = data["systemConfig"]["description"]
        except Exception:
            pass
        custom_packages = ""
        try:
            with open(f) as fh:
                data = yaml.safe_load(fh)
            extra = data.get("systemConfig", {}).get("packages", []) if data else []
            base_packages = estimate_base_packages(f.name, extra)
            custom_count = len(extra) - base_packages
            if custom_count > 0:
                custom_packages = f" (+{custom_count} custom pkgs)"
        except Exception:
            pass
        print(f"  {f.name:<55s} {desc}{custom_packages}")
    print()


def estimate_base_packages(fname, current_packages):
    """Rough estimate: templates average 10-12 packages."""
    return len(current_packages) - 3  # rough guess if we can't find the base


def resolve_template_name(base_template):
    """Find the base template file. Accept short name or full path."""
    base_dir = find_base_dir()
    if not base_dir:
        print("ERROR: No image-templates/ directory found.")
        sys.exit(1)

    # If it's already a full path that exists
    p = Path(base_template)
    if p.exists():
        return p

    # Try relative to base_dir
    p2 = base_dir / base_template
    if p2.exists():
        return p2

    # Try appending .yml
    if not base_template.endswith(".yml"):
        p3 = base_dir / f"{base_template}.yml"
        if p3.exists():
            return p3

    print(f"ERROR: Template '{base_template}' not found in {base_dir}")
    print("Use --list-base to see available templates.")
    sys.exit(1)


def add_packages(data, packages_str):
    """Add packages to systemConfig.packages, deduplicating."""
    new_pkgs = [p.strip() for p in packages_str.split(",") if p.strip()]
    existing = data.get("systemConfig", {}).get("packages", [])
    if not existing:
        if "systemConfig" not in data:
            data["systemConfig"] = {}
        if "packages" not in data["systemConfig"]:
            data["systemConfig"]["packages"] = []

    for pkg in new_pkgs:
        if pkg not in data["systemConfig"]["packages"]:
            data["systemConfig"]["packages"].append(pkg)

    return new_pkgs


def add_repo(data, repo_url, codename=None, component="main", pkey=None):
    """Add a package repository to packageRepositories."""
    if "packageRepositories" not in data:
        data["packageRepositories"] = []

    # Parse the repo string: "url codename component" or a full URL
    entry = {
        "url": repo_url,
    }

    if codename:
        entry["codename"] = codename
    if pkey:
        entry["pkey"] = pkey
    entry["component"] = component

    # Check for duplicates
    for existing in data["packageRepositories"]:
        if existing.get("url") == entry["url"]:
            print(f"  Repo already exists, skipping: {repo_url}")
            return False

    data["packageRepositories"].append(entry)
    return True


def update_metadata(data, output_name, description=None):
    """Update metadata and image name in the template."""
    # Update image name
    if "image" not in data:
        data["image"] = {}
    data["image"]["name"] = output_name

    # Update or add metadata section
    if "metadata" not in data:
        data["metadata"] = {}
    if isinstance(data["metadata"], dict):
        if description:
            data["metadata"]["description"] = description
        else:
            # Auto-generate description
            data["metadata"]["description"] = f"Custom image: {output_name}"

    # Update systemConfig name to match
    sc = data.get("systemConfig", {})
    if isinstance(sc, dict):
        sc["name"] = clean_name(output_name)


def build_image(output_name):
    """Call image-composer-tool to build the customized template."""
    output_path = USER_TEMPLATES_DIR / f"{output_name}.yml"
    if not output_path.exists():
        print(f"ERROR: Template not found: {output_path}")
        return False

    cwd = Path.cwd()
    build_cmd = f"cd {cwd} && sudo -E ./image-composer-tool build {output_path}"

    print(f"\nBuilding: {output_path.name}")
    print(f"Command: {build_cmd}")
    print()

    result = subprocess.run(
        ["sudo", "-E", str(cwd / "image-composer-tool"), "build", str(output_path)],
        cwd=str(cwd),
        capture_output=False,
    )
    return result.returncode == 0


def main():
    parser = argparse.ArgumentParser(
        description="Customize an image-composer template with extra packages and repos",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  %(prog)s ubuntu24-x86_64-minimal-raw.yml --name my-ros2-image --add-packages "ros-jazzy-ros-base,ros-jazzy-demo-nodes-py" --add-repo "http://packages.ros.org/ros2/ubuntu noble main"
  %(prog)s ubuntu24-x86_64-dlstreamer.yml --name my-dlstreamer --add-packages "vim,git,htop" --build
  %(prog)s --list-base
  %(prog)s --list
""")

    # Base template
    parser.add_argument("base_template", nargs="?", help="Base template filename (e.g. ubuntu24-x86_64-minimal-raw.yml)")

    # Options
    parser.add_argument("--name", help="Output template name (without .yml). Required unless --list or --list-base")
    parser.add_argument("--desc", "--description", dest="description", help="Custom description for the new template")
    parser.add_argument("--add-packages", help="Comma-separated list of extra packages to add")
    parser.add_argument("--add-repo", action="append", dest="add_repos", help="Repository to add (format: url [codename [component]]). Can use multiple times.")
    parser.add_argument("--add-repo-key", help="GPG key URL for added repos")
    parser.add_argument("--repo-codename", help="Codename for --add-repo (e.g. noble, ubuntu24, jammy)")
    parser.add_argument("--repo-component", default="main", help="Component for --add-repo (default: main)")

    # Actions
    parser.add_argument("--list-base", action="store_true", help="List available base templates")
    parser.add_argument("--list", dest="list_user", action="store_true", help="List existing user-customized templates")
    parser.add_argument("--build", action="store_true", help="Build the image immediately after customization")

    args = parser.parse_args()

    # Handle list actions
    if args.list_base:
        list_base_templates()
        return

    if args.list_user:
        list_user_templates()
        return

    # Validate required args
    if not args.base_template:
        parser.print_help()
        print("\nERROR: base_template is required. Use --list-base to see available templates.")
        sys.exit(1)

    if not args.name:
        parser.print_help()
        print("\nERROR: --name is required to specify the output template name.")
        sys.exit(1)

    # Sanitize output name
    output_name = clean_name(args.name)
    if output_name != args.name:
        print(f"Sanitized output name: {output_name}")

    # Resolve base template
    base_path = resolve_template_name(args.base_template)
    print(f"Base template: {base_path}")

    # Ensure user templates dir exists
    USER_TEMPLATES_DIR.mkdir(parents=True, exist_ok=True)
    output_path = USER_TEMPLATES_DIR / f"{output_name}.yml"

    # Load base template
    with open(base_path) as f:
        data = yaml.safe_load(f)

    if not data:
        print("ERROR: Could not parse base template YAML")
        sys.exit(1)

    # ---- Customization ----
    changes = []

    # Update metadata / image name
    update_metadata(data, output_name, args.description)
    changes.append(f"Image name: {output_name}")

    # Add extra packages
    if args.add_packages:
        added = add_packages(data, args.add_packages)
        if added:
            changes.append(f"Packages added: {', '.join(added)}")

    # Add extra repos
    if args.add_repos:
        for repo_str in args.add_repos:
            parts = repo_str.split()
            url = parts[0]
            codename = parts[1] if len(parts) > 1 else (args.repo_codename or "noble")
            component = parts[2] if len(parts) > 2 else args.repo_component
            pkey = args.add_repo_key
            if add_repo(data, url, codename, component, pkey):
                changes.append(f"Repo added: {url} ({codename}/{component})")

    # Write output template
    with open(output_path, "w") as f:
        yaml.dump(data, f, default_flow_style=False, sort_keys=False, allow_unicode=True, width=120)

    print(f"\nTemplate saved: {output_path}")
    print()

    print("Changes made:")
    for c in changes:
        print(f"  + {c}")

    # Optionally build
    if args.build:
        print("\n" + "=" * 60)
        print("BUILDING IMAGE...")
        print("=" * 60)
        success = build_image(output_name)
        if success:
            print("\nImage build completed successfully!")
        else:
            print("\nImage build FAILED — check the output above for details.")
            sys.exit(1)
    else:
        print(f"\nTo build: cd {Path.cwd()} && sudo -E ./image-composer-tool build {output_path}")


if __name__ == "__main__":
    main()
