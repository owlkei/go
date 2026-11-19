---
description: Security sweep with the go-security agent - latest Go and dependency vulnerabilities, toolchain security releases, and backend code flaws. `audit` reports only; `fix` updates dependencies and applies small patches.
argument-hint: "[audit | fix] [package or area]"
---

# Security: $ARGUMENTS

The first word is the mode, `audit` when missing; the rest narrows the code review to a package or an
area (`middlewares`, `websocket`, `trace`). Dependency and toolchain checks always cover the
whole module.

Run `go-security` with the mode, the scope and today's date (so it knows what "latest" means).
Pass on its report. In `fix` mode, check its work yourself before reporting: `git diff --stat` shows
only `go.mod`, `go.sum` and the files it named, `govulncheck ./...` reports nothing reachable (or
only what it explained), and `make check` passes. Don't commit: the user commits with `/commit`.

When to run it:

- before tagging a release;
- after any dependency or Go version change;
- on a schedule (for example weekly with `/schedule`), since new advisories appear without any change
  in this repository; CI also runs `govulncheck` on every push and pull request.
