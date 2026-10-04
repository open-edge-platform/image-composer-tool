# CLAUDE.md — image-composer-tool

@AGENTS.md

The file above is auto-imported into every Claude Code session in this repo — no
need to ask for it. It covers architecture, build/test/lint, project conventions,
security, and anti-patterns. This file only states rules that are stricter than,
or missing from, AGENTS.md.

---

## Git commits & PRs (overrides AGENTS.md wording)

- Every commit must carry a **cryptographic SSH signature** (`gpg.format = ssh`),
  not a DCO `Signed-off-by:` trailer. This is per-clone git config, not something
  the repo ships with — each contributor sets it up once: `git config gpg.format
  ssh`, `git config user.signingkey <path-to-your-ssh-public-key>`, and
  `git config commit.gpgsign true` (after that, `git commit` signs automatically —
  no need to pass `-S` manually). Verify with `git log --format='%h %G? %s'`
  (`G` = good, `N` = unsigned). Note `git cherry-pick`/`git rebase` drop
  signatures unless `commit.gpgsign=true` is set or `--gpg-sign` is passed.

## Hooks

`AGENTS.md` documents `.github/hooks/*.json`, which is the config format GitHub
Copilot reads. Claude Code instead reads `.claude/settings.json` (team-shared,
committed) — this repo wires it to call the **same** scripts under
`.github/hooks/scripts/` so behavior stays identical across both tools:
block dangerous git commands, flag `gofmt` drift / invalid templates after
edits, and ask before `git push` when the branch diff vs `main` exceeds ~500
LOC. Don't add logic directly in `.claude/settings.json` — extend the shared
scripts instead so GHCP and Claude Code can't drift apart.

