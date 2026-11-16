## What and why

<!-- One or two sentences: what changes and why. Link the issue, roadmap step (docs/roadmap/) or design (docs/design/). -->

## Type

<!-- Keep the ones that apply. The PR title follows Conventional Commits: type(scope): description -->

- feat · fix · perf · refactor · test · docs · build · ci · chore

## Public API and behavior

<!--
"None", or list every change users can see: exported names added / removed / renamed, signatures,
status codes, response bodies, defaults, bootstrap panics. Mark breaking changes.
New exported names need a reason: Ginject keeps its public surface small (CLAUDE.md › Product principles).
-->

## How it was verified

<!-- Tests added or changed, and for request-path code the benchmark before and after. -->

```
go test -run '^$' -bench . -benchmem -count 5 ./<package>/   # before and after, compared with benchstat
```

## Checklist

- [ ] `make check` passes (gofmt, `go vet`, golangci-lint, `go test -race`)
- [ ] New behavior has tests; a bug fix has a test that failed before the fix
- [ ] Shared state is protected and backed by a `-race` test
- [ ] Hot-path changes (per request or message) come with benchmark numbers; no `unsafe`, new `sync.Pool` or hand-managed buffers without an approved design
- [ ] Follows `.ginject/conventions.md`: no comments in Go code, the file rule (exported API in topic files, shared helpers in `fn.go`), `test.DiffMessage` in tests
- [ ] Docs match the code: the package `README.md`, and `CLAUDE.md` / `.ginject/` when a concept changed
- [ ] No new dependency in `go.mod` (or it is justified above)
- [ ] Security: limits on client-controlled input, no internal details in responses or logs of secrets

## Notes for reviewers

<!-- Anything that needs attention: trade-offs, follow-ups, parts to read first. -->
