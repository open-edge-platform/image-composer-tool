# ICT Tool Security Objectives

The ICT tool enables you to build minimal, verifiable, and secure
operating system images so you can reduce the attack surface of images and
simplify the adoption of modern boot mechanisms. The tool lets you encrypt
partitions, use dm-verity for root protection, and support Secure Boot.

## 1. Reduced Attack Surface

The ICT tool allows customization, enabling the inclusion of only
necessary kernel features and executables, reducing the overall attack surface
as defined by the user.

## 2. Secure Image Generation

To generate secure images, the tool optionally supports the following:

* Cryptographic signing of the boot chain (kernel, initial RAM disk, kernel
  command line) for Secure Boot, ensuring integrity and authenticity.
* Protect the root filesystem with dm-verity, making offline attacks
  more difficult.
* Generate a Software Bill of Materials (SBOM) for each image, providing
  transparency for its components in SPDX format which can be used with tools
  like `gradle` and `Maven`

## 3. Support for Modern Boot Mechanisms

The tool simplifies the adoption of Unified Kernel Image (UKI), a modern boot
mechanism, potentially improving edge node security by supporting secure boot.

## 4. Partition Customization

The ICT tool allows you to customize the partition layout,
including optionally encrypting root partitions, customizing partition sizes,
and adding partitions for security.

## 5. Web UI Package Search Verification

The web UI's package picker (`GET /packages/search`) verifies a repository's
`Release` metadata against a local GPG keyring when the repository catalog
supplies one that exists on disk. Every other repository family is searched
unverified because it has no genuine local trust anchor available today —
its signing key is fetched from a remote URL, is an rpm repository signed with
a same-origin key rather than a separately trusted keyring, or has no key
configured at all.
## 6. Deployed-System Provisioning

The provisioning settings applied during installation
(`systemConfig.users[].sshAuthorizedKeys`, `proxy`, `provisioning`,
`cloudInit`, `aptPolicy`) are designed so template input cannot escape the
image or corrupt generated configuration:

* Files the provisioning step generates (proxy settings, SSH keys, systemd
  units, cloud-init datasource, APT policy) are written into the image through
  an `os.Root` handle, so a symlink in the image (for example a home directory
  pointing at a host path) cannot redirect a write outside the image root.
  SSH key destinations (home, `.ssh`, `authorized_keys`) must not be symlinks
  at all. Script and cloud-init host files are placed by the general
  `additionalFiles` copy, which is not confined this way; like every other
  additional file, they trust the destination path inside the image.
* Script destinations that collide with generated files, or with each other,
  are rejected so a script's payload is never silently replaced.
* Host files referenced by these sections (scripts, cloud-init seed files, SSH
  key files) must be regular files, and symlinks are rejected when the template
  is loaded. This does not extend to the general `additionalFiles` entries,
  whose source files are checked with `os.Stat` and so follow symlinks.
* Proxy URLs, `noProxy`, script names and paths, and user `shell`/`home` are
  validated before they reach generated files or commands. Quotes, whitespace,
  line breaks, `..` traversal, and other values that could break out of a line
  or path are rejected. SSH key entries must each be a single line.
* SSH keys are installed with the ownership and modes sshd's `StrictModes`
  requires (`~/.ssh` `0700`, `authorized_keys` `0600`). Only public keys are
  accepted: each line must be an OpenSSH public key, so a private key file
  passed by mistake is rejected, and validation errors never echo key content.
* Proxy URLs with embedded credentials, query strings, or fragments are rejected, because the proxy is
  written to world-readable files and to the template carried on the ISO.
  Repository and package URLs in the composition manifest have credentials,
  query strings, and fragments removed.
* A user with sudo access (`sudo: true`, or membership of the `sudo` or `wheel`
  group) and no password is never left with an empty one. With
  SSH keys, the password is locked and the user gets passwordless sudo, limited
  to that user and written to `/etc/sudoers.d/90-ict-key-only-admins` (mode
  0440); without keys, the build stops before the account is created.
* The APT upgrade policy limits which repositories can replace installed
  packages. The distribution's security updates stay enabled unless the image
  is immutable.
* A `password` value that is already a crypt(3) hash is always set as a hash,
  so a pre-hashed password can never become the literal login password.
* The template carried on the ISO references provisioning inputs through
  their on-ISO copies; build-host paths of scripts, cloud-init files, and SSH
  key files are not included.

## 7. Credentials Supplied Through the Web UI

A template may declare a privileged account with no password and no SSH key —
the build is then refused rather than creating an account with an empty
password. The Web UI can supply the missing credential (`credentials` on
`POST /templates/compose` and `POST /builds`), which means an account password
crosses the API. It is handled so the secret's lifetime is the build's:

* The requirement is derived from the resolved template's own
  `systemConfig.users`, and a supplied `user` must already be declared there.
  A credential never creates an account, matching the CLI's
  `--ssh-authorized-key` contract.
* A password is hashed with SHA-512 crypt on the build host before it is
  written anywhere, so the plain text exists only in memory. A value that is
  already a crypt(3) hash is passed through rather than hashed again. The
  password is fed to `openssl` on standard input, never as a command argument,
  so it does not appear in the process table or in any logged command string.
* The hash is carried in the generated `extends` delta, written `0600` and
  removed when the build ends. It is redacted from the compose response (both
  the resolved template and the delta view), from the archived template served
  by `GET /builds/{id}/template`, and from the build record's command line.
  The build log is not scrubbed; ICT's own account-creation path does not log
  the value.
* An SSH public key is validated with the OpenSSH `authorized_keys` parser,
  limited to one key per entry, and rejected if it looks like a private key.
  Public keys are not redacted, being public.
* The server binds `127.0.0.1` by default and has no authentication, so a
  password posted to it is exposed to any local process able to reach the
  port. Binding it to a routable address (`--host`) sends credentials over an
  unauthenticated, unencrypted channel and should not be combined with this
  feature.
* `openssl` was added to the shell allowlist for the hashing step. It was
  already required by the in-chroot `hash_algo` path, which could not run
  without it.
