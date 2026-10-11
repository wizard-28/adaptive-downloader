#!/usr/bin/env bash
# Build the clean public release tree from a committed revision.
# Usage: scripts/make_public_release.sh OUTDIR [REV]   (REV defaults to HEAD)
# - git archive honours .gitattributes export-ignore (internal agent/handoff docs,
#   team status, Word/PDF files, devcontainer are excluded);
# - README.public.md becomes README.md;
# - untracked files (transcripts, local binaries) are never included;
# - SHA256SUMS lists every file in the release tree.
set -euo pipefail
out="${1:?usage: make_public_release.sh OUTDIR [REV]}"
rev="${2:-HEAD}"
rm -rf "$out"; mkdir -p "$out"
git archive --format=tar "$rev" | tar -x -C "$out"
mv "$out/README.public.md" "$out/README.md"
rm -f "$out/SHA256SUMS"
( cd "$out" && find . -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum > SHA256SUMS )
echo "release tree: $out ($(find "$out" -type f | wc -l) files); revision $(git rev-parse "$rev")"
