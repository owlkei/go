---
description: Read-only impact report of the staged changes against the last commit. Unstaged work is stashed first so only what will be committed is built, tested and reviewed.
argument-hint: "(no arguments)"
---

# Check staged changes

Never commit, push, or change what is staged.

## Steps

1. `git diff --staged --stat`. Empty: tell the user nothing is staged and stop.
2. **Stash unstaged work safely.** Record `git rev-parse -q --verify refs/stash` (may be empty), run
   `git stash push --keep-index --include-untracked -m "check-staged"`, then read the ref again. A stash was
   created only if the ref changed: otherwise there is nothing to pop later. Popping without a new stash
   would apply an older, unrelated stash.
3. **Verify** the tree that will be committed: `go build ./...`, `go vet ./...`, `golangci-lint run` and
   `go test -race` on the packages the diff touches. Failures are findings.
4. **Review** `git diff --staged` with the checklist in `.claude/agents/go-reviewer.md` (correctness,
   concurrency, framework conventions, security, tests, docs). Also list every exported API that was
   added, removed or changed.
5. **Restore**: if step 2 created a stash, `git stash pop`. If the pop fails, stop and tell the user their
   work is in `git stash list` under "check-staged". Restoring their work comes before the report.

## Report

Write it in the language the user writes in, once (no second translation):

```
## Staged change impact

Build / vet / lint / tests: pass or fail (failing items)

### Files
| File | + | - | Kind |

### Findings
| # | File:line | Finding | Severity |

### Conclusion
Behavior change vs last commit: YES or NO, plus one or two sentences.
```

Severity: **critical** (behavior change not intended, broken API, race, failing build or test),
**warning** (missing error handling or test, leaked internals, performance regression),
**info** (naming, dead code). One line per finding. The conclusion says YES or NO, never "maybe".
