# Error Demo — Non-Existent Package

## Setup

A custom template was created with a deliberately wrong package name:

```bash
python3 customize-template.py robotics-demo-ubuntu24-x86_64.yml \
  --name ros2-error-test \
  --add-packages "this-package-does-not-exist-at-all,nano"
```

The script accepted it silently — **no validation at customization time**.

## Build Failure

The build ran cleanly through metadata resolution (150,858 packages across
13 repos), then failed at dependency resolution:

```
requested package '"this-package-does-not-exist-at-all"' not found in repo
found 50 packages in request of 51
```

The 50 valid packages (ROS2, system pkgs, nano) resolved fine. The one bogus
name killed the entire build.

**Result:** Exit code 1. No disk image created — failure is fast and early
(before any partition or image work starts).

## Diagnostic Output

The tool writes a JSON report of all missing packages:

```
builds/Missing_Requested_Packages_<timestamp>.json
```

## Key Takeaway

| Stage | Validation |
|-------|-----------|
| `customize-template.py` | None — accepts any string |
| `image-composer-tool build` | Catches missing packages at dependency resolution |
| Container image | Never created — fails before disk writes begin |

Always pre-check package names with `apt-cache search <name>` before building,
especially for third-party repos where package names can differ from what
you expect.
