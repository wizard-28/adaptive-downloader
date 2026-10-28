#!/usr/bin/env python3
"""Find a section of the split project context.

docs/PROJECT_CONTEXT.md is an index; the text lives in docs/context/*.md.
Usage:
  python3 tools/context_find.py 22.109      # file:line of "### 22.109 ..."
  python3 tools/context_find.py 20           # file:line of "## 20. ..."
  python3 tools/context_find.py -s lockin    # grep -i across all context files
Print the section itself with:  sed -n 'START,ENDp' <file>  (or use the Read tool).
"""
import glob
import re
import sys

FILES = sorted(glob.glob("docs/context/*.md"))


def find(sec):
    lvl = "###" if "." in sec else "##"
    pat = re.compile(r"^%s %s(\.|\s)" % (lvl, re.escape(sec)) if lvl == "##" else r"^%s %s\s" % (lvl, re.escape(sec)))
    hits = []
    for f in FILES:
        for n, line in enumerate(open(f, encoding="utf-8"), 1):
            if pat.match(line):
                hits.append((f, n, line.strip()))
    return hits


def search(term):
    t = term.lower()
    for f in FILES:
        for n, line in enumerate(open(f, encoding="utf-8"), 1):
            if t in line.lower():
                print(f"{f}:{n}: {line.rstrip()[:160]}")


if __name__ == "__main__":
    a = sys.argv[1:]
    if len(a) == 2 and a[0] == "-s":
        search(a[1])
    elif len(a) == 1:
        h = find(a[0])
        for f, n, l in h:
            print(f"{f}:{n}: {l}")
        if not h:
            sys.exit("not found")
    else:
        sys.exit(__doc__)
