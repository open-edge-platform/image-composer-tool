# CLAUDE.md — image-composer-tool

@AGENTS.md

The file above is auto-imported into every Claude Code session in this repo — no
need to ask for it. It covers architecture, build/test/lint, project conventions,
security, and anti-patterns. This file only states rules that are stricter than,
or missing from, AGENTS.md.

---

## Git commits & PRs (overrides AGENTS.md wording)

- Every commit must carry a **cryptographic SSH signature** (`gpg.format = ssh`),
  not a DCO `Signed-off-by:` trailer. `commit.gpgsign=true` is already set in
  this repo's local git config, so `git commit` signs automatically — no need
  to pass `-S` manually here. Verify with `git log --format='%h %G? %s'`
  (`G` = good, `N` = unsigned). Note `git cherry-pick`/`git rebase` drop
  signatures unless `commit.gpgsign=true` is set or `--gpg-sign` is passed.
