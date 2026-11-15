# Design docs

One file per feature or API change, written by the `ginject-architect` agent and approved by the
maintainer before any test or code is written (`/feature`).

- **Name:** `NNNN-<slug>.md`, numbered in order (`0001-rate-limit-guard.md`).
- **Status:** `Proposed` → `Approved` (by the maintainer) → `Implemented` (after merge). A design
  that changes an approved one is a new file that says which one it supersedes.
- The code wins over a design once it is implemented: fix the code or write a new design, don't
  let the two drift.

## Template

```markdown
# NNNN: <title>

Status: Proposed
Packages: <the packages this touches>

## Goal
What the user can do after this change, in two or three sentences.

## Public API
Every exported type, function and method added or changed, as Go signatures.

## Behavior
Numbered rules (B1, B2, …) that tests can assert: inputs, outputs, errors, panics
(bootstrap panics as "<category>: <detail>"), ordering, defaults.

## Concurrency
Goroutines, shared state, how each is protected, lock order, how goroutines stop.

## Security
Limits, validation, secrets, anything an attacker controls.

## Impact
Callers that change, compatibility, docs to update (README, CLAUDE.md, .ginject/).

## Test plan
For each rule: the test name and the case (happy path, zero/nil, boundaries, invalid,
hostile input, concurrent use).

## Open questions
Decisions the maintainer must make before approval. Empty when there are none.
```
