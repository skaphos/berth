#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Rillan AI LLC
# SPDX-License-Identifier: MIT
set -euo pipefail
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
fixture_dir="$(mktemp -d "${TMPDIR:-/tmp}/berth-release-notes.XXXXXXXXXX")"
trap 'rm -rf -- "$fixture_dir"' EXIT
cat > "$fixture_dir/CHANGELOG.md" <<'CHANGELOG'
# Changelog
## [Unreleased]
Future work.
## [0.4.1](https://example.invalid/compare) (2026-09-07)

### Bug Fixes
* First reviewed fix.
* Second reviewed fix.

## [0.4.0](https://example.invalid/old)
Old notes.
CHANGELOG
"$script_dir/release-notes.sh" 0.4.1 "$fixture_dir/CHANGELOG.md" > "$fixture_dir/actual"
printf '\n### Bug Fixes\n* First reviewed fix.\n* Second reviewed fix.\n\n' > "$fixture_dir/expected"
diff -u "$fixture_dir/expected" "$fixture_dir/actual"
if "$script_dir/release-notes.sh" 0.4.2 "$fixture_dir/CHANGELOG.md" 2>/dev/null; then
  echo 'Missing version unexpectedly accepted' >&2; exit 1
fi
printf '## [0.4.1]\n\n## [0.4.0]\nOld notes.\n' > "$fixture_dir/empty"
if "$script_dir/release-notes.sh" 0.4.1 "$fixture_dir/empty" 2>/dev/null; then
  echo 'Empty version unexpectedly accepted' >&2; exit 1
fi
cat "$fixture_dir/CHANGELOG.md" "$fixture_dir/CHANGELOG.md" > "$fixture_dir/duplicate"
if "$script_dir/release-notes.sh" 0.4.1 "$fixture_dir/duplicate" 2>/dev/null; then
  echo 'Duplicate version unexpectedly accepted' >&2; exit 1
fi
printf '## [0.4.1-rc.1]\nCandidate notes.\n' > "$fixture_dir/prerelease"
"$script_dir/release-notes.sh" 0.4.1-rc.1 "$fixture_dir/prerelease" > "$fixture_dir/actual"
printf 'Candidate notes.\n' > "$fixture_dir/expected"
diff -u "$fixture_dir/expected" "$fixture_dir/actual"
echo 'Release note extraction tests passed.'
