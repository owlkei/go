#!/usr/bin/env bash
# Stop: before Claude reports done, vet and test the Go packages it changed.
# Exit 2 sends the failure back to Claude, which keeps working instead of stopping.
set -uo pipefail

input=$(cat)
# Claude is already continuing because of this hook: don't loop forever.
[[ $(jq -r '.stop_hook_active // false' <<<"$input") == "true" ]] && exit 0

cd "${CLAUDE_PROJECT_DIR:-.}" || exit 0

changed=$( { git diff --name-only HEAD -- '*.go'; git ls-files --others --exclude-standard -- '*.go'; } 2>/dev/null | sort -u)
[[ -z "$changed" ]] && exit 0

pkgs=$(xargs -n1 dirname <<<"$changed" | sort -u | while read -r dir; do
  [[ -d "$dir" ]] && echo "./$dir"
done)
[[ -z "$pkgs" ]] && exit 0

# shellcheck disable=SC2086
if ! out=$(go vet $pkgs 2>&1 && golangci-lint run $pkgs 2>&1 && go test -race $pkgs 2>&1); then
  echo "Verification failed for the changed packages ($(tr '\n' ' ' <<<"$pkgs")):" >&2
  tail -n 60 <<<"$out" >&2
  exit 2
fi
exit 0
