# ADR: Supplying a Build-Time Credential for a Privileged Account from the Web UI

**Status**: Accepted
**Date**: 2026-10-08
**Authors**: ICT Team
**Technical Area**: Web UI / API / Config

---

## Summary

Some templates (e.g. EdgePack's unattended ISOs) declare a privileged `admin`
account with a placeholder public-key file that ships empty, so credential
validation rejects the build. The Web UI now lets a user supply a password or
SSH public key per build. The password is hashed on the host
(`openssl passwd -6` over stdin) the moment it is received; only the hash is
ever written to disk, in a generated `extends` delta, and the plaintext is
never logged, archived, or persisted.

---

## Context

The API spawns a fixed `ict build <template>` with no channel for a
credential, so a UI build of such a template failed only after minutes of
package download, with a generic error about an empty password. The CLI
equivalent (`--ssh-authorized-key`) has no analog in the API, and the
requirement (which accounts need a credential, and whether the one supplied
satisfies it) is derivable entirely from the template's own user
configuration, so it does not need a new manifest field.

---

## Decision

1. **Detect the requirement from the merged template, not the manifest.** A
   shared "is this account privileged, and does it already have a credential"
   predicate is used by both the compose response and the build-time
   validation, so they can never disagree about which accounts need one.
2. **Hash on the host, immediately.** The password is piped to
   `openssl passwd -6` on stdin (never argv) and an already-hashed value is
   passed through unchanged. No code path writes a plaintext password to
   disk.
3. **Carry the credential through the existing `extends` delta**, not a new
   build mechanism. The same pipeline already carries package/disk
   overrides (see
   [`adr-web-ui-advanced-mode-extends.md`](adr-web-ui-advanced-mode-extends.md)).
   The delta's user is merged into the template's by name.
4. **Fail fast, server-side.** Credential validation runs during
   compose/build resolution, before any chroot work starts, returning a
   distinct `CREDENTIALS_REQUIRED` code rather than the generic `NO_MATCH`, so
   the UI can prompt instead of surfacing a build-log failure minutes later.
5. **Redact everywhere a template is displayed or archived**, including the
   delta YAML returned to the client (a pre-existing gap this closes).

### How the backend determines the requirement, and the API surface it needs

The underlying rejection (a privileged account with no password and no SSH
key is invalid) is not new: it already exists and already runs deep in image
building and in the CLI's validate command. What is missing today is that
same check at the point the Web UI's build actually starts, so the API path
currently finds out only after minutes of package installation. This
decision adds the check at two points, both server-side, ahead of that late
failure:

1. **On compose.** The Web UI already calls the compose endpoint as soon as a
   selection's dropdowns resolve to a template, and again after any override
   (including a newly supplied credential). The backend scans the resolved
   template's users for a privileged account missing both a password and an
   SSH key, and reports it in the response.
2. **On build.** The Web UI re-composes once more immediately before sending
   the actual build request, which carries the same resolved template plus
   whatever credential the user supplied. The backend runs the identical
   check against that build-bound template before anything starts, so a
   stale or bypassed compose response can never let an unsatisfied build
   through: the compose response is a preview, not the authority.

The compose request/response gain:

- **Request**: an optional list of per-user credential inputs (a username, and
  a password or an SSH public-key line), accepted alongside the rest of the
  overrides the compose endpoint already takes.
- **Response**: a list of accounts that are privileged and still missing a
  credential after any supplied inputs are applied, each naming the account
  so the UI knows who to prompt for and whether the prompt is still needed.

Supplying a satisfying credential clears that account from the response's
list; it is not a one-shot flag the client has to track itself. No new
endpoint is introduced: this rides on the existing compose/build request and
response shapes.

---

## Consequences

**Good**

- No new CLI flag, argv, or build mechanism; reuses the override pipeline
  verified for package/disk changes.
- UI and backend validation share one predicate, so they cannot drift apart.
- Plaintext exists only in memory for the duration of one hash call.

**Trade-offs**

- Password is still accepted in plaintext over the API request body, which is
  acceptable only because the server binds to localhost by default.
- Credential state must stay out of the persisted UI store (transient,
  per-build): an easy rule to accidentally violate when extending the form.

---

## Alternatives considered

**A new `--password-file`/`--ssh-authorized-key` style CLI flag threaded
through the API's build invocation.** Rejected: the build command line is
logged, streamed, and persisted unredacted, so adding user-derived arguments
there reopens exactly the leak this ADR closes, and duplicates SSH-key
validation that already exists for the CLI flag.

**A manifest-level credential-requirement flag.** Rejected: the requirement
is a property of the template's `systemConfig.users`, already fully
expressed; a manifest flag would be redundant data that can drift from the
template it describes.

---

## References

- [`adr-web-ui-advanced-mode-extends.md`](adr-web-ui-advanced-mode-extends.md)
