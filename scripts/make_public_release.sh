#!/usr/bin/env bash
# Build the clean public release tree from a COMMITTED revision, then verify it.
# Usage: scripts/make_public_release.sh OUTDIR [REV]   (REV defaults to HEAD)
#  - git archive honours .gitattributes export-ignore (internal agent/handoff
#    docs, team status, Word/PDF files, devcontainer);
#  - README.public.md becomes README.md;
#  - untracked files (transcripts, local binaries) are never included;
#  - the build FAILS if a forbidden path/pattern is present in the tree;
#  - SHA256SUMS lists every file; tools/claims_check.py + its self-test run
#    inside the release tree.
set -euo pipefail
out="${1:?usage: make_public_release.sh OUTDIR [REV]}"
rev="${2:-HEAD}"
rm -rf "$out"; mkdir -p "$out"
git archive --format=tar "$rev" | tar -x -C "$out"
mv "$out/README.public.md" "$out/README.md"

# --- leak scan -----------------------------------------------------------
bad=0
for f in docs/team_status_2026-09-29.md transcript.txt .devcontainer; do
  if [ -e "$out/$f" ]; then echo "LEAK: $f present in release tree"; bad=1; fi
done
if find "$out" \( -name '*.docx' -o -name '*.pdf' -o -name 'transcript*' \) -print | grep -q .; then
  echo "LEAK: office/pdf/transcript file present:"; find "$out" \( -name '*.docx' -o -name '*.pdf' -o -name 'transcript*' \) -print; bad=1
fi
# compiled binaries (ELF)
while IFS= read -r -d '' f; do
  if head -c4 "$f" 2>/dev/null | grep -q $'\x7fELF'; then echo "LEAK: binary $f"; bad=1; fi
done < <(find "$out" -type f -size +100k -print0)
[ "$bad" -eq 0 ] || { echo "release build FAILED leak scan"; exit 1; }

# --- checksums and verification ----------------------------------------------
rm -f "$out/SHA256SUMS"
( cd "$out" && find . -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum > SHA256SUMS )
( cd "$out" && python3 tools/claims_check.py && python3 tools/test_claims_check.py )
echo "release tree: $out ($(find "$out" -type f | wc -l) files); revision $(git rev-parse "$rev")"
