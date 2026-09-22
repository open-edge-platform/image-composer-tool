# image-composer-tool (ICT) Contributor Guide

The following are guidelines for contributing to the image-composer-tool
project, including the code of conduct, submitting issues, and contributing
code.

## Table of Contents

- [image-composer-tool (ICT) Contributor Guide](#image-composer-tool-ict-contributor-guide)
  - [Table of Contents](#table-of-contents)
  - [Code of Conduct](#code-of-conduct)
  - [Security](#security)
  - [Get Started](#get-started)
  - [How to Contribute](#how-to-contribute)
    - [Contribute Code Changes](#contribute-code-changes)
    - [Improve Documentation](#improve-documentation)
    - [Report Bugs](#report-bugs)
    - [Suggest Enhancements](#suggest-enhancements)
    - [Submit Pull Requests](#submit-pull-requests)
  - [Development Guidelines](#development-guidelines)
    - [Coding Standards](#coding-standards)
    - [Commit Messages and Pull Requests](#commit-messages-and-pull-requests)
    - [Testing](#testing)
  - [Branching \& Release Strategy](#branching--release-strategy)
    - [Hotfix policy](#hotfix-policy)
  - [Sign Your Work](#sign-your-work)
  - [License](#license)

## Code of Conduct

This project and everyone participating in it are governed by the
[`CODE_OF_CONDUCT`](CODE_OF_CONDUCT.md) document. By participating, you are
expected to adhere to this code.

## Security

Read the [Security Policy](SECURITY.md).

## Get Started

Clone the repository and follow the [README](README.md) for build and usage
instructions.

```text
    git clone https://github.com/open-edge-platform/image-composer-tool.git
    cd image-composer-tool
```

Build and test locally before opening a pull request:

```text
    earthly +build
    earthly +test-quick
    earthly +lint
```

If Earthly is unavailable, fall back to `go build ./...`, `go test ./...`,
and `golangci-lint run`.

## How to Contribute

### Contribute Code Changes

> If you want to help improve ICT, choose one of the issues reported in
> [GitHub Issues](https://github.com/open-edge-platform/image-composer-tool/issues)
> and create a [Pull Request](https://github.com/open-edge-platform/image-composer-tool/pulls)
> to address it.
> Note: Please check that the change hasn't been implemented before you
> start working on it.

### Improve Documentation

The easiest way to help is to review the guides under `docs/` and provide
feedback on existing articles. Whether you notice a mistake, see an
opportunity to improve the text, or think more information should be added,
open an issue or pull request to discuss the change.

### Report Bugs

If you encounter a bug, open an issue in
[GitHub Issues](https://github.com/open-edge-platform/image-composer-tool/issues).
Provide the following information to help us understand and resolve the
issue quickly:

- A clear and descriptive title
- A thorough description of the issue
- Steps to reproduce the issue (including the image template used, if
  applicable)
- Expected versus actual behavior
- Relevant logs (redact any sensitive paths/tokens)
- Your environment (OS/arch, image type being built)

### Suggest Enhancements

Suggestions for new OS providers, image types, or features are welcome.
Follow these steps to make a suggestion:

- Check if there's already a similar suggestion in
  [GitHub Issues](https://github.com/open-edge-platform/image-composer-tool/issues).
- If not, open a new issue and provide:
  - A clear and descriptive title
  - A detailed description of the enhancement
  - Use cases and benefits
  - Any additional context or references

### Submit Pull Requests

Before submitting a pull request, ensure you follow these guidelines:

- Fork the repository and branch from `main` (branch prefixes:
  `feature/`, `fix/`, `docs/`, `refactor/`).
- Follow the [Development Guidelines](#development-guidelines) below.
- Test your changes thoroughly (`earthly +test-quick` and `earthly +lint`).
- Update relevant documentation under `docs/` in the same PR (see the
  documentation table in [.github/copilot-instructions.md](.github/copilot-instructions.md)).
- Fill in the [pull request template](.github/PULL_REQUEST_TEMPLATE.md) when
  submitting.
- Wait for a review. Maintainers will review your pull request and provide
  feedback. You can expect a merge once your changes are validated by
  automated tests/checks and approved by maintainers.

## Development Guidelines

### Coding Standards

Consistently following coding standards helps maintain readability and
quality. Adhere to the following conventions:

- Follow the project's
  [coding style guide](docs/user-guide/architecture/image-composer-tool-coding-style.md)
  (line length, function length, error handling, logging).
- Use the project logger (`internal/utils/logger`), not `fmt.Println` or
  stdlib `log`.
- Use `network.GetSecureHTTPClient()` for HTTP calls, never
  `http.DefaultClient`.
- Use `internal/utils/shell` (allowlisted commands) for shell execution,
  never raw `exec.Command`.
- Code must pass `golangci-lint` (`govet`, `gofmt`, `errcheck`,
  `staticcheck`, `unused`, `gosimple`).

### Commit Messages and Pull Requests

Clear and informative commit messages make it easier to understand the
history of the project. Follow these guidelines:

- Use [Conventional Commits](https://www.conventionalcommits.org/):
  `type(scope): description` (`feat`, `fix`, `docs`, `test`, `refactor`,
  `chore`, `build`, `ci`, `perf`).
- Use the present tense (e.g., "Add feature" not "Added feature").
- Keep the subject line concise.

Please fill in the details as per the
[pull request template](.github/PULL_REQUEST_TEMPLATE.md) while submitting
the pull request.

### Testing

Thorough testing is crucial to maintain project stability. Ensure that you:

- Write unit tests for new and existing code using stdlib `testing` only
  (no testify), table-driven with `t.Run()`.
- Use `t.TempDir()` for filesystem tests and `t.Parallel()` where safe.
- Run tests locally before submitting a pull request (`earthly +test-quick`
  or `go test ./...`).
- Check code coverage against `.coverage-threshold` and aim to raise it,
  not lower it.

## Branching & Release Strategy

- All feature and fix PRs target `main` directly; there is no separate
  development branch to rebase onto.
- `main` is expected to build and pass CI at all times.
- Ahead of each quarterly release, a short-lived `release-YYYY.Q` branch
  (e.g. `release-2026.3`) is cut from `main` for final validation. Only
  release-blocking fixes are cherry-picked into it; `main` keeps
  accepting new feature work in parallel.
- Releases are tagged on the release branch (e.g. `2026.3.0`), and any
  fixes made there are merged back into `main`.
- If you need your change included in an upcoming release, note this in
  your pull request description; maintainers will flag it for
  cherry-picking during the freeze window.

### Hotfix policy

- Only the **latest release branch** and `main` are actively maintained.
- Bugs and security issues reported against older release branches will
  not be individually backported; they are fixed only in the latest
  release branch (and `main`, if still applicable there).
- If you hit an issue on an older release, upgrade to the latest release
  branch or `main` first to confirm whether it's already fixed before
  filing a report.

## Sign Your Work

Every commit must be cryptographically signed. Configure Git once per
clone to sign with your SSH key:

```text
    git config gpg.format ssh
    git config user.signingkey <path-to-your-ssh-public-key>
    git config commit.gpgsign true
```

After this one-time setup, `git commit` signs automatically. Verify with:

```text
    git log --format='%h %G? %s'
```

(`G` = good signature, `N` = unsigned). Note that `git cherry-pick` and
`git rebase` drop signatures unless `commit.gpgsign=true` is set or
`--gpg-sign` is passed explicitly.

## License

By contributing to this project, you agree that your contributions will be
licensed under the [MIT](LICENSE) license of the repository.
