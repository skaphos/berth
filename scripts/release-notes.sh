#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Rillan AI LLC
# SPDX-License-Identifier: MIT
set -euo pipefail

version="${1:?usage: release-notes.sh VERSION [CHANGELOG]}"
changelog="${2:-CHANGELOG.md}"
upgrade_dir="${3:-$(dirname "$changelog")/docs/releases}"
if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.+-]+)?$ ]]; then
  echo "Invalid release version: $version" >&2
  exit 1
fi

# Release Please emits version sections as level-two bracketed headings.
# Fail before publishing if the tag has no unique, nonempty reviewed section.
awk -v version="$version" '
  /^## / {
    active = 0
    heading = "## [" version "]"
    if (substr($0, 1, length(heading)) == heading &&
        (length($0) == length(heading) || substr($0, length(heading)+1, 1) ~ /[ (]/)) {
      count++
      active = 1
    }
    next
  }
  active {
    notes = notes $0 "\n"
    if ($0 ~ /[^[:space:]]/) nonempty = 1
  }
  END {
    if (count != 1 || !nonempty) {
      print "Expected one nonempty changelog section for " version > "/dev/stderr"
      exit 1
    }
    printf "%s", notes
  }
' "$changelog"

# Version-specific migration text is reviewed in source alongside the fixes.
# Keep it outside the generated changelog so release-please cannot overwrite it.
upgrade_notes="$upgrade_dir/$version.md"
if [ -f "$upgrade_notes" ]; then
  if [ ! -s "$upgrade_notes" ]; then
    echo "Empty upgrade notes for $version" >&2
    exit 1
  fi
  printf '\n'
  cat "$upgrade_notes"
fi
