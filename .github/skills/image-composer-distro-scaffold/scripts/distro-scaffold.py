#!/usr/bin/env python3
"""Scaffold and validate lean new-distro onboarding assets."""

from __future__ import annotations

import argparse
import re
import subprocess
import sys
from pathlib import Path

import yaml

VALID_IMAGE_TYPES = {"raw", "iso", "initrd", "img"}


def find_project_root() -> Path:
    cwd = Path.cwd().resolve()
    for parent in [cwd] + list(cwd.parents):
        if (parent / "go.mod").exists() and (parent / "internal" / "provider").exists():
            return parent
    raise FileNotFoundError("Could not find project root with go.mod and internal/provider")


def parse_csv(value: str) -> list[str]:
    return [item.strip() for item in value.split(",") if item.strip()]


def validate_identifier(name: str, field_name: str) -> None:
    if not re.fullmatch(r"[a-z0-9][a-z0-9_-]*", name):
        raise ValueError(
            f"Invalid {field_name} '{name}'. Use lowercase letters, numbers, '_' or '-' and start with alnum."
        )


def default_config_filename(image_type: str, arch: str) -> str:
    if image_type in {"img", "initrd"}:
        return f"default-initrd-{arch}.yml"
    return f"default-{image_type}-{arch}.yml"


def build_paths(root: Path, args: argparse.Namespace) -> dict[str, Path | list[Path]]:
    provider_dir = root / "internal" / "provider" / args.provider_dir
    provider_file = provider_dir / f"{args.provider_dir}.go"
    defaults_dir = (
        root
        / "config"
        / "osv"
        / args.os
        / args.dist
        / "imageconfigs"
        / "defaultconfigs"
    )

    default_files = []
    for image_type in args.image_types:
        for arch in args.archs:
            default_files.append(defaults_dir / default_config_filename(image_type, arch))

    return {
        "provider_dir": provider_dir,
        "provider_file": provider_file,
        "defaults_dir": defaults_dir,
        "default_files": sorted(default_files),
        "build_go": root / "cmd" / "image-composer-tool" / "build.go",
    }


def dist_root(root: Path, os_name: str, dist: str) -> Path:
    return root / "config" / "osv" / os_name / dist


def infer_release_version_from_dist(dist: str) -> str | None:
    match = re.fullmatch(r"[a-z]+(\d{2})", dist)
    if not match:
        return None
    return f"{match.group(1)}.04"


def detect_provider_alias(root: Path, os_name: str) -> str:
    provider_dir = root / "internal" / "provider"
    if (provider_dir / os_name).exists():
        return os_name
    return os_name


def detect_reference_dist(root: Path, os_name: str, requested_dist: str) -> str:
    os_root = root / "config" / "osv" / os_name
    if not os_root.exists():
        raise FileNotFoundError(f"OS config root not found: {os_root}")

    candidates = []
    for child in os_root.iterdir():
        if not child.is_dir():
            continue
        if child.name == requested_dist:
            continue
        if (child / "providerconfigs").exists() and (child / "imageconfigs" / "defaultconfigs").exists():
            candidates.append(child.name)

    if not candidates:
        raise FileNotFoundError(
            f"No reference dist found under {os_root}. Use --from-dist to provide a source dist explicitly."
        )

    requested_num = None
    req_digits = re.findall(r"\d+", requested_dist)
    if req_digits:
        requested_num = int(req_digits[-1])

    numbered = []
    unnumbered = []
    for dist in candidates:
        digits = re.findall(r"\d+", dist)
        if digits:
            numbered.append((int(digits[-1]), dist))
        else:
            unnumbered.append(dist)

    if numbered:
        if requested_num is not None:
            numbered.sort(key=lambda item: (abs(item[0] - requested_num), item[0]))
            return numbered[0][1]
        numbered.sort()
        return numbered[-1][1]

    unnumbered.sort()
    return unnumbered[-1]


def deep_replace_dist_tokens(data: object, src_dist: str, dst_dist: str) -> object:
    if isinstance(data, dict):
        return {key: deep_replace_dist_tokens(value, src_dist, dst_dist) for key, value in data.items()}
    if isinstance(data, list):
        return [deep_replace_dist_tokens(value, src_dist, dst_dist) for value in data]
    if isinstance(data, str):
        return data.replace(src_dist, dst_dist)
    return data


def copy_dist_tree(root: Path, os_name: str, source_dist: str, target_dist: str, force: bool) -> Path:
    src_root = dist_root(root, os_name, source_dist)
    dst_root = dist_root(root, os_name, target_dist)

    if not src_root.exists():
        raise FileNotFoundError(f"Source dist does not exist: {src_root}")

    if dst_root.exists() and not force:
        raise FileExistsError(f"Refusing to overwrite existing dist root: {dst_root}")

    if dst_root.exists() and force:
        for path in sorted(dst_root.rglob("*"), reverse=True):
            if path.is_file() or path.is_symlink():
                path.unlink()
            elif path.is_dir():
                path.rmdir()
        dst_root.rmdir()

    for src_path in src_root.rglob("*"):
        rel = src_path.relative_to(src_root)
        dst_path = dst_root / rel
        if src_path.is_dir():
            dst_path.mkdir(parents=True, exist_ok=True)
            continue

        dst_path.parent.mkdir(parents=True, exist_ok=True)
        if src_path.suffix in {".yml", ".yaml", ".list"}:
            text = src_path.read_text(encoding="utf-8")
            dst_path.write_text(text.replace(source_dist, target_dist), encoding="utf-8")
        else:
            dst_path.write_bytes(src_path.read_bytes())

    config_path = dst_root / "config.yml"
    if config_path.exists():
        config_data = yaml.safe_load(config_path.read_text(encoding="utf-8")) or {}
        config_data = deep_replace_dist_tokens(config_data, source_dist, target_dist)
        release_version = infer_release_version_from_dist(target_dist)
        if release_version and isinstance(config_data, dict):
            for arch_cfg in config_data.values():
                if isinstance(arch_cfg, dict):
                    arch_cfg["releaseVersion"] = release_version

        config_path.write_text(yaml.safe_dump(config_data, sort_keys=False), encoding="utf-8")

    return dst_root


def update_schema_dist_enum(root: Path, os_name: str, dist: str) -> bool:
    schema_path = root / "internal" / "config" / "schema" / "os-image-template.schema.json"
    schema = yaml.safe_load(schema_path.read_text(encoding="utf-8"))
    changed = False

    # Support both legacy "definitions" and current "$defs" schema layouts.
    target_ref = (
        schema.get("$defs", {})
        .get("Target", {})
        .get("allOf", [])
    )
    if not target_ref:
        target_ref = (
            schema.get("definitions", {})
            .get("Target", {})
            .get("allOf", [])
        )
    for rule in target_ref:
        rule_os = (((rule.get("if") or {}).get("properties") or {}).get("os") or {}).get("const")
        if rule_os != os_name:
            continue

        enum_values = ((((rule.get("then") or {}).get("properties") or {}).get("dist") or {}).get("enum") or [])
        if dist not in enum_values:
            enum_values.append(dist)
            enum_values.sort()
            rule["then"]["properties"]["dist"]["enum"] = enum_values
            changed = True
        break

    if changed:
        import json

        schema_path.write_text(json.dumps(schema, indent=2) + "\n", encoding="utf-8")

    return changed


def update_os_config_schema_dist_enum(root: Path, dist: str) -> bool:
    schema_path = root / "internal" / "config" / "schema" / "os-config.schema.json"
    schema = yaml.safe_load(schema_path.read_text(encoding="utf-8"))
    changed = False

    arch_props = (schema.get("patternProperties", {}) or {}).get("^(x86_64|aarch64|armv7hl)$", {})
    dist_props = (arch_props.get("properties", {}) or {}).get("dist", {})
    enum_values = dist_props.get("enum", [])

    if isinstance(enum_values, list) and dist not in enum_values:
        enum_values.append(dist)
        enum_values.sort()
        schema["patternProperties"]["^(x86_64|aarch64|armv7hl)$"]["properties"]["dist"]["enum"] = enum_values
        changed = True

    if changed:
        import json

        schema_path.write_text(json.dumps(schema, indent=2) + "\n", encoding="utf-8")

    return changed


def create_pilot_user_template(root: Path, os_name: str, dist: str, arch: str, image_type: str, force: bool) -> Path:
    target = root / "user-templates" / f"{dist}-{arch}-minimal-{image_type}.yml"
    content = {
        "image": {
            "name": f"{dist}-{arch}-minimal",
            "version": "1.0",
        },
        "target": {
            "os": os_name,
            "dist": dist,
            "arch": arch,
            "imageType": image_type,
        },
        "systemConfig": {
            "name": "minimal",
            "packages": ["bash"],
        },
    }
    write_file(target, yaml.safe_dump(content, sort_keys=False), force)
    return target


def create_example_image_template(root: Path, os_name: str, dist: str, arch: str, image_type: str, force: bool) -> Path:
    target = root / "image-templates" / f"{dist}-{arch}-minimal-{image_type}.yml"
    release_version = infer_release_version_from_dist(dist) or "1.0.0"
    content = {
        "metadata": {
            "description": f"Minimal {dist} example template scaffolded from existing {os_name} provider defaults",
            "use_cases": [
                f"Quick-start {dist} image builds",
                "Template validation and onboarding checks",
            ],
            "keywords": ["minimal", os_name, dist, arch, image_type],
        },
        "image": {
            "name": f"{dist}-{arch}-minimal",
            "version": release_version,
        },
        "target": {
            "os": os_name,
            "dist": dist,
            "arch": arch,
            "imageType": image_type,
        },
    }
    write_file(target, yaml.safe_dump(content, sort_keys=False), force)
    return target


def provider_file_content(provider_dir: str, os_name: str) -> str:
    return f'''package {provider_dir}

import (
    "fmt"

    "github.com/open-edge-platform/image-composer-tool/internal/config"
    "github.com/open-edge-platform/image-composer-tool/internal/provider"
    "github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
    "github.com/open-edge-platform/image-composer-tool/internal/utils/system"
)

const OsName = "{os_name}"

var log = logger.Logger()

// Provider implements provider.Provider for scaffolding new distro support.
type Provider struct{{}}

func Register(targetOs, targetDist, targetArch string) error {{
    provider.Register(&Provider{{}}, targetDist, targetArch)
    return nil
}}

func (p *Provider) Name(dist, arch string) string {{
    return system.GetProviderId(OsName, dist, arch)
}}

func (p *Provider) Init(dist, arch string) error {{
    return fmt.Errorf("{provider_dir} provider Init is not implemented")
}}

func (p *Provider) PreProcess(template *config.ImageTemplate) error {{
    return fmt.Errorf("{provider_dir} provider PreProcess is not implemented")
}}

func (p *Provider) BuildImage(template *config.ImageTemplate) error {{
    return fmt.Errorf("{provider_dir} provider BuildImage is not implemented")
}}

func (p *Provider) PostProcess(template *config.ImageTemplate, err error) error {{
    return err
}}
'''


def default_config_content(os_name: str, dist: str, image_type: str, arch: str) -> str:
    return f'''# Scaffolded default config stub.
# TODO: Fill complete defaults for {os_name}/{dist} {image_type} {arch}.

systemConfig:
  name: "{dist}-default-{image_type}-{arch}"
  description: "Scaffold default config stub"
  packages: []
'''


def write_file(path: Path, content: str, force: bool) -> None:
    if path.exists() and not force:
        raise FileExistsError(f"Refusing to overwrite existing file: {path}")
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content, encoding="utf-8")


def run_scaffold(root: Path, args: argparse.Namespace) -> int:
    paths = build_paths(root, args)
    print("Scaffold plan:")
    if args.use_existing_provider:
        print(f"  provider mode:  existing ({paths['provider_dir']})")
    else:
        print(f"  provider dir:   {paths['provider_dir']}")
        print(f"  provider file:  {paths['provider_file']}")
    print(f"  defaults dir:   {paths['defaults_dir']}")
    for default_file in paths["default_files"]:
        print(f"  default file:   {default_file}")

    if args.dry_run:
        print("\nDry run enabled. No files created.")
        return 0

    if not args.use_existing_provider:
        write_file(
            paths["provider_file"],
            provider_file_content(args.provider_dir, args.os),
            args.force,
        )

    for image_type in args.image_types:
        for arch in args.archs:
            default_file = paths["defaults_dir"] / default_config_filename(image_type, arch)
            write_file(
                default_file,
                default_config_content(args.os, args.dist, image_type, arch),
                args.force,
            )

    print("\nScaffold completed.")
    return 0


def run_quick_add(root: Path, args: argparse.Namespace) -> int:
    provider_alias = detect_provider_alias(root, args.os)
    source_dist = args.from_dist or detect_reference_dist(root, args.os, args.dist)

    print("Quick-add plan:")
    print(f"  os:              {args.os}")
    print(f"  new dist:        {args.dist}")
    print(f"  reference dist:  {source_dist}")
    print(f"  provider alias:  {provider_alias}")

    if args.dry_run:
        print("\nDry run enabled. No files created.")
        return 0

    dst_root = copy_dist_tree(root, args.os, source_dist, args.dist, args.force)
    schema_changed = update_schema_dist_enum(root, args.os, args.dist)
    os_config_schema_changed = update_os_config_schema_dist_enum(root, args.dist)

    example_template = None
    if not args.skip_example_template:
        example_template = create_example_image_template(
            root,
            args.os,
            args.dist,
            args.arch,
            args.image_type,
            args.force,
        )

    pilot_template = None
    if args.create_pilot_template:
        pilot_template = create_pilot_user_template(
            root,
            args.os,
            args.dist,
            args.arch,
            args.image_type,
            args.force,
        )

    print("\nQuick-add completed.")
    print(f"  dist root:       {dst_root}")
    print(f"  schema updated:  {schema_changed}")
    print(f"  os-config schema updated: {os_config_schema_changed}")
    if example_template:
        print(f"  image template:  {example_template}")
    if pilot_template:
        print(f"  pilot template:  {pilot_template}")
    print("\nNext: run quick-validate for a full structural and template validation check.")
    return 0


def run_quick_validate(root: Path, args: argparse.Namespace) -> int:
    provider_alias = detect_provider_alias(root, args.os)
    dist_dir = dist_root(root, args.os, args.dist)
    defaults_dir = dist_dir / "imageconfigs" / "defaultconfigs"
    failures: list[str] = []

    check(dist_dir.exists(), f"dist root exists: {dist_dir}", failures)
    check((dist_dir / "config.yml").exists(), f"dist config exists: {dist_dir / 'config.yml'}", failures)
    providerconfigs_dir = dist_dir / "providerconfigs"
    check(providerconfigs_dir.exists(), f"providerconfigs directory exists: {providerconfigs_dir}", failures)
    check(
        providerconfigs_dir.exists() and any(providerconfigs_dir.glob("*.yml")),
        f"providerconfigs has yml files: {providerconfigs_dir}",
        failures,
    )
    chrootenv_dir = dist_dir / "chrootenvconfigs"
    check(chrootenv_dir.exists(), f"chrootenvconfigs directory exists: {chrootenv_dir}", failures)
    check(
        chrootenv_dir.exists() and any(chrootenv_dir.glob("*.yml")),
        f"chrootenvconfigs has yml files: {chrootenv_dir}",
        failures,
    )
    additionalfiles_dir = dist_dir / "imageconfigs" / "additionalfiles"
    check(additionalfiles_dir.exists(), f"additionalfiles directory exists: {additionalfiles_dir}", failures)

    if failures:
        print("\nQuick validation failed with", len(failures), "structural issue(s).")
        return 1

    image_types = []
    if defaults_dir.exists():
        for path in defaults_dir.glob("default-*-*.yml"):
            parts = path.name.replace("default-", "", 1).rsplit("-", 1)
            if len(parts) == 2:
                image_types.append(parts[0])
    image_types = sorted(set(image_types or [args.image_type]))

    validate_args = argparse.Namespace(
        os=args.os,
        dist=args.dist,
        provider_dir=provider_alias,
        provider_alias=provider_alias,
        archs=[args.arch],
        image_types=image_types,
        check_user_template=args.check_user_template,
        run_template_validate=args.run_template_validate,
        run_template_validate_merged=args.run_template_validate_merged,
        use_existing_provider=True,
    )
    return run_validate(root, validate_args)


def check(condition: bool, label: str, failures: list[str]) -> None:
    prefix = "PASS" if condition else "FAIL"
    print(f"[{prefix}] {label}")
    if not condition:
        failures.append(label)


def run_template_validate(root: Path, template_path: Path, merged: bool) -> tuple[bool, str]:
    bin_path = root / "image-composer-tool"
    log_file = "/tmp/image-composer-tool-validate.log"
    commands: list[tuple[list[str], Path]] = []
    commands.append(([
        "go",
        "run",
        "./cmd/image-composer-tool",
        "--log-file",
        log_file,
        "validate",
        str(template_path),
    ], root))
    if bin_path.exists() and bin_path.is_file():
        commands.append(([
            str(bin_path),
            "--log-file",
            log_file,
            "validate",
            str(template_path),
        ], template_path.parent))

    last_output = ""
    for command, run_cwd in commands:
        if merged:
            command.insert(-1, "--merged")

        result = subprocess.run(
            command,
            cwd=str(run_cwd),
            capture_output=True,
            text=True,
            check=False,
        )

        output = (result.stdout or "") + (result.stderr or "")
        last_output = output.strip()
        if result.returncode == 0:
            return True, last_output

    return False, last_output


def run_validate(root: Path, args: argparse.Namespace) -> int:
    paths = build_paths(root, args)
    failures: list[str] = []

    provider_file_text = ""
    if paths["provider_file"].exists():
        provider_file_text = Path(paths["provider_file"]).read_text(encoding="utf-8")

    check(Path(paths["provider_dir"]).exists(), f"provider directory exists: {paths['provider_dir']}", failures)
    check(Path(paths["provider_file"]).exists(), f"provider file exists: {paths['provider_file']}", failures)
    os_name_match = re.search(r'const\s*\(.*?\bOsName\s*=\s*"([^"]+)".*?\)', provider_file_text, re.S)
    if not os_name_match:
        os_name_match = re.search(r'\bOsName\s*=\s*"([^"]+)"', provider_file_text)

    if args.use_existing_provider:
        check(
            bool(os_name_match and os_name_match.group(1) == args.os),
            f"existing provider OsName matches target.os ({args.os})",
            failures,
        )
    else:
        check(
            bool(os_name_match and os_name_match.group(1) == args.os),
            f"OsName constant matches target.os ({args.os})",
            failures,
        )
        check("func Register(" in provider_file_text, "provider Register function exists", failures)
        check("provider.Register(" in provider_file_text, "provider.Register call exists", failures)

    build_go_text = ""
    if Path(paths["build_go"]).exists():
        build_go_text = Path(paths["build_go"]).read_text(encoding="utf-8")

    check(
        f"case {args.provider_alias}.OsName:" in build_go_text,
        f"build.go switch includes case {args.provider_alias}.OsName",
        failures,
    )
    check(
        f"{args.provider_alias}.Register(" in build_go_text,
        f"build.go calls {args.provider_alias}.Register",
        failures,
    )

    check(Path(paths["defaults_dir"]).exists(), f"defaults directory exists: {paths['defaults_dir']}", failures)
    for default_file in paths["default_files"]:
        check(default_file.exists(), f"default config exists: {default_file}", failures)

    if args.check_user_template:
        template_path = Path(args.check_user_template)
        if not template_path.is_absolute():
            template_path = root / template_path

        check(template_path.exists(), f"user template exists: {template_path}", failures)
        if template_path.exists():
            data = yaml.safe_load(template_path.read_text(encoding="utf-8")) or {}
            target = data.get("target", {}) if isinstance(data, dict) else {}
            check(target.get("os") == args.os, f"user template target.os matches ({args.os})", failures)
            check(target.get("dist") == args.dist, f"user template target.dist matches ({args.dist})", failures)

            if args.run_template_validate:
                ok, output = run_template_validate(root, template_path, args.run_template_validate_merged)
                validate_mode = "merged validate" if args.run_template_validate_merged else "validate"
                check(ok, f"image-composer-tool {validate_mode} succeeded for {template_path}", failures)
                if output:
                    print("\nvalidate output:")
                    print(output)

    if failures:
        print("\nValidation failed with", len(failures), "issue(s).")
        return 1

    print("\nValidation passed.")
    return 0


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Scaffold and validate lean new-distro onboarding assets")
    subparsers = parser.add_subparsers(dest="mode", required=True)

    def add_shared_args(p: argparse.ArgumentParser) -> None:
        p.add_argument("--os", required=True, help="target.os value (e.g. my-new-os)")
        p.add_argument("--dist", required=True, help="target.dist value (e.g. mydist1)")
        p.add_argument("--provider-dir", required=True, help="internal/provider directory name")
        p.add_argument("--provider-alias", required=True, help="build.go import/switch alias for provider")
        p.add_argument("--archs", required=True, help="comma-separated arch list (e.g. x86_64,aarch64)")
        p.add_argument("--image-types", required=True, help="comma-separated image types (raw,iso,initrd,img)")

    scaffold = subparsers.add_parser("scaffold", help="Create provider and config/osv scaffold files")
    add_shared_args(scaffold)
    scaffold.add_argument("--dry-run", action="store_true", help="Print changes without writing files")
    scaffold.add_argument("--force", action="store_true", help="Overwrite existing files")
    scaffold.add_argument(
        "--use-existing-provider",
        action="store_true",
        help="Create only config/osv defaults and keep the existing provider package unchanged",
    )

    validate = subparsers.add_parser("validate", help="Validate provider/config structure and optional user template")
    add_shared_args(validate)
    validate.add_argument(
        "--check-user-template",
        help="Optional path to a user template to validate target mapping and run CLI validation",
    )
    validate.add_argument(
        "--run-template-validate",
        action="store_true",
        help="Run image-composer-tool validate against --check-user-template",
    )
    validate.add_argument(
        "--run-template-validate-merged",
        action="store_true",
        help="Use --merged when running image-composer-tool validate",
    )
    validate.add_argument(
        "--use-existing-provider",
        action="store_true",
        help="Validate against an existing provider package instead of a newly scaffolded provider",
    )

    quick_add = subparsers.add_parser(
        "quick-add",
        help="One-command dist onboarding for existing providers (copy full dist tree + schema update)",
    )
    quick_add.add_argument("--os", required=True, help="target.os value (e.g. ubuntu)")
    quick_add.add_argument("--dist", required=True, help="new target.dist value (e.g. ubuntu22)")
    quick_add.add_argument("--from-dist", help="reference dist to clone (auto-detected if omitted)")
    quick_add.add_argument("--arch", default="x86_64", help="pilot template arch (default: x86_64)")
    quick_add.add_argument("--image-type", default="raw", help="pilot template image type (default: raw)")
    quick_add.add_argument(
        "--skip-example-template",
        action="store_true",
        help="skip creating image-templates/<dist>-<arch>-minimal-<imageType>.yml",
    )
    quick_add.add_argument("--create-pilot-template", action="store_true", help="create a user pilot template")
    quick_add.add_argument("--dry-run", action="store_true", help="Print actions without writing files")
    quick_add.add_argument("--force", action="store_true", help="Overwrite existing dist directory or template")

    quick_validate = subparsers.add_parser(
        "quick-validate",
        help="One-command validation for a quick-added dist using existing provider mode",
    )
    quick_validate.add_argument("--os", required=True, help="target.os value (e.g. ubuntu)")
    quick_validate.add_argument("--dist", required=True, help="target.dist value (e.g. ubuntu22)")
    quick_validate.add_argument("--arch", default="x86_64", help="arch for validation (default: x86_64)")
    quick_validate.add_argument("--image-type", default="raw", help="fallback image type (default: raw)")
    quick_validate.add_argument("--check-user-template", help="Optional user template path for validation")
    quick_validate.add_argument(
        "--run-template-validate",
        action="store_true",
        help="Run image-composer-tool validate against --check-user-template",
    )
    quick_validate.add_argument(
        "--run-template-validate-merged",
        action="store_true",
        help="Use --merged when running image-composer-tool validate",
    )

    return parser


def normalize_args(args: argparse.Namespace) -> None:
    validate_identifier(args.os, "os")
    validate_identifier(args.dist, "dist")

    if args.mode in {"scaffold", "validate"}:
        validate_identifier(args.provider_dir, "provider-dir")
        validate_identifier(args.provider_alias, "provider-alias")

        args.archs = parse_csv(args.archs)
        args.image_types = parse_csv(args.image_types)

        if not args.archs:
            raise ValueError("--archs must contain at least one value")
        if not args.image_types:
            raise ValueError("--image-types must contain at least one value")

        invalid_types = [value for value in args.image_types if value not in VALID_IMAGE_TYPES]
        if invalid_types:
            raise ValueError(f"Unsupported image types: {', '.join(invalid_types)}")

    if args.mode == "quick-add":
        validate_identifier(args.arch, "arch")
        if args.from_dist:
            validate_identifier(args.from_dist, "from-dist")
        if args.image_type not in VALID_IMAGE_TYPES:
            raise ValueError(f"Unsupported image type: {args.image_type}")

    if args.mode == "quick-validate":
        validate_identifier(args.arch, "arch")
        if args.image_type not in VALID_IMAGE_TYPES:
            raise ValueError(f"Unsupported image type: {args.image_type}")

    if getattr(args, "run_template_validate_merged", False) and not getattr(args, "run_template_validate", False):
        raise ValueError("--run-template-validate-merged requires --run-template-validate")


def main() -> int:
    parser = build_parser()
    args = parser.parse_args()

    try:
        normalize_args(args)
        root = find_project_root()

        if args.mode == "scaffold":
            return run_scaffold(root, args)
        if args.mode == "validate":
            return run_validate(root, args)
        if args.mode == "quick-add":
            return run_quick_add(root, args)
        if args.mode == "quick-validate":
            return run_quick_validate(root, args)

        parser.error(f"Unknown mode: {args.mode}")
        return 2
    except Exception as err:  # pylint: disable=broad-except
        print(f"ERROR: {err}")
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
