# GPG Key Verification Failure — Workaround

## Symptom

Build fails with:

```
Error: pre-processing failed: failed to download image packages:
failed to download packages: user package fetch failed:
parsing user repo failed: failed to verify release file:
signature verification failed (tried both armored and binary):
openpgp: invalid data: tag byte does not have MSB set
```

The GPG key URL downloads successfully (HTTP 200) but the tool
cannot verify the Release file signature against it.

## Root Cause

The `pkey` field in a `packageRepositories` entry points to a GPG
key URL. Some repo providers distribute keys in a format the
image-composer-tool's GPG verification logic doesn't handle
correctly — particularly binary `.gpg` keys for third-party repos.

Intel DL Streamer repo example that triggers this:

```yaml
  - codename: "ubuntu24"
    url: "https://apt.repos.intel.com/edgeai/dlstreamer/ubuntu24"
    pkey: "https://apt.repos.intel.com/edgeai/dlstreamer/GPG-PUB-KEY-INTEL-DLS.gpg"
```

## Fix

Replace `pkey` with `[trusted=yes]` to skip GPG verification
for that specific repo:

```yaml
  - codename: "ubuntu24"
    url: "https://apt.repos.intel.com/edgeai/dlstreamer/ubuntu24"
    pkey: "[trusted=yes]"
```

This tells apt to trust the repo without verifying its Release
file signature. The repo's Packages metadata is still checksum-
verified against the Release file's SHA256 hashes, so package
integrity is maintained.

## When to Use

- The `pkey` URL returns HTTP 200 and downloads a plausible key
  file, but the tool's GPG verification still fails
- The repo is a trusted source (official Intel, NVIDIA, Docker repos)
- You've confirmed the Release file exists and contains correct
  checksums — verify with `curl -sIL <url>/dists/<codename>/Release.gpg`
