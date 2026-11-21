#!/bin/sh
# Fails when a tracked file holds personal data: an address at a personal mail provider or an
# absolute path inside a home directory. The repository is public, so whatever is committed is
# published. Run by `make check` and CI.
set -eu

found=0

check() {
	label=$1
	pattern=$2
	if hits=$(git grep -nIE "$pattern" -- . ':!go.sum' ':!**/go.sum' ':!yarn.lock'); then
		echo "$label:"
		echo "$hits"
		found=1
	fi
}

check "personal email address" \
	'[A-Za-z0-9._%+-]+@(gmail|googlemail|yahoo|outlook|hotmail|live|icloud|me|mac|proton|protonmail|gmx|yandex|qq|163)\.[A-Za-z.]+'
check "home directory path" \
	'(/Users/|/home/|[A-Za-z]:\\Users\\)[A-Za-z0-9._-]+'

if [ "$found" -ne 0 ]; then
	echo "remove personal data from the files above (use a GitHub noreply address, a relative path or \$HOME)"
	exit 1
fi
