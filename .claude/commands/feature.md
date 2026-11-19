---
description: Build a feature or API change test-first with the agent team - architect designs, the user approves, tester writes failing tests, builder makes them pass, reviewer checks.
argument-hint: "[--fable | --opus] <feature description>"
---

# Feature (TDD): $ARGUMENTS

You orchestrate; the agents do the work. Each agent starts with no context, so every prompt you send
names the design file and the packages, and points at the `CLAUDE.md` sections it needs. Pass on what
the previous agent reported only when the next one needs it.

Small changes (a bug fix, a private helper, no public API) don't need this flow: use `/implement`.

## 1. Design: `go-architect`

Ask it to design the feature (`$ARGUMENTS` without the flag). It writes
`docs/design/NNNN-<slug>.md` (Status: Proposed).

**Pick the architect's model** (the agent file defaults to `opus`; pass the model override on the call),
first rule that applies:

1. `--fable` or `--opus` in `$ARGUMENTS`: use it.
2. **Near the weekly reset → `fable`.** Read `Fable weekly reset:` in `CLAUDE.local.md` (weekday and
   time, the user's local zone). If the next reset is less than 48 hours away (`date` gives now), use
   `fable`: quota left unused at the reset is lost.
3. **Complex → `fable`.** Any of: it changes the DI container, routing, the request pipeline or
   WebSocket lifecycle (`core/`, `common/`, `routing/`, `ctx/`); it needs a concurrency design (new
   goroutines, shared state, locks); it is security-sensitive (auth, CSRF / CORS, crypto, limits, file
   paths); it spans more than one package or changes an exported API that callers use; it is the
   escalation after the builder failed twice.
4. **Otherwise → `opus`**: one package, additive API, following an existing pattern (another
   middleware, guard or module option like the ones in the repository).

Tell the user which model you picked and why, in one line. If `fable` fails or is unavailable (quota,
error), run the same call again with `opus` and say so. If `CLAUDE.local.md` has no reset line, skip
rule 2 and ask the user once to add it.

## 2. Approval (the user)

Show the user the design file path, its public API, the behavior rules and its open questions.
**Stop and wait.** Apply the changes they ask for (yourself, or through the architect for larger
ones). When they approve, set `Status: Approved` in the file.

## 3. Red: `go-tester`

Ask it to write the tests for the approved design. Then check it yourself:

- `git status --porcelain` shows only `*_test.go` files besides the design. Revert anything else.
- `go test -race` on the packages fails, and only for the reasons the tester reported (identifiers
  the design adds, assertions on new behavior). Tests of existing behavior pass.

Record the test files' state: `git ls-files -m -o --exclude-standard -- '*_test.go' | sort | xargs shasum > "$TMPDIR/feature-tests.sha"`.

## 4. Green: `go-builder`

Ask it to implement the design until the tests pass. Then check:

- The tests are untouched: `shasum -c "$TMPDIR/feature-tests.sha"` passes and
  `git ls-files -m -o --exclude-standard -- '*_test.go'` lists the same files. If a test changed,
  reject the work: restore the tests and send the builder back.
- `make check` passes.

If the builder stops on a wrong test or an unclear design, or the checks still fail after a second
attempt, send its report to `go-architect`: it decides whether the design or the tests must
change. Apply its decision without stopping to ask (changed tests go through step 3 again) and note
every deviation from the approved design in the design file and in your final report.

## 5. Review: `go-reviewer`

Ask it to review the working tree against the design. Fix critical findings through the builder (test
changes through the tester), then run `make check` again.

## 6. Hand over (the user)

Set the design's status to `Implemented` only after the user merges. Report in the language the user
writes in: the design file, the tests per behavior rule, the files changed, the `make check` result,
and the review findings left open. Don't commit: the user commits with `/commit` when ready.
