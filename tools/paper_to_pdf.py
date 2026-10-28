#!/usr/bin/env python3
"""Render reports/paper_draft.md as a readable PDF (reports/paper/paper_draft.pdf).

A small converter for the subset of Markdown the draft uses: headings, paragraphs,
bullet and numbered lists (two levels), pipe tables, images, **bold**, *italic*,
`code` and [link](target). It writes reports/paper/paper_draft.tex and runs
pdflatex twice. The Markdown file stays the source of truth; this is a view.

Usage: python3 tools/paper_to_pdf.py
"""
import os
import re
import subprocess

SRC = "reports/paper_draft.md"
OUTDIR = "reports/paper"
TEX = os.path.join(OUTDIR, "paper_draft.tex")

PRE = r"""\documentclass[10pt,a4paper]{article}
\usepackage[T1]{fontenc}
\usepackage[utf8]{inputenc}
\usepackage{mathptmx}
\usepackage[scaled=0.9]{helvet}
\usepackage{courier}
\usepackage{textcomp,newunicodechar}
\newunicodechar{×}{\ensuremath{\times}}
\newunicodechar{→}{\ensuremath{\rightarrow}}
\newunicodechar{−}{\ensuremath{-}}
\newunicodechar{÷}{\ensuremath{\div}}
\newunicodechar{±}{\ensuremath{\pm}}
\newunicodechar{≥}{\ensuremath{\geq}}
\newunicodechar{≤}{\ensuremath{\leq}}
\newunicodechar{ε}{\ensuremath{\varepsilon}}
\usepackage[margin=2.2cm]{geometry}
\usepackage{graphicx,booktabs,tabularx,longtable,array,enumitem,xcolor}
\usepackage[hidelinks]{hyperref}
\usepackage{titlesec}
\titleformat{\section}{\sffamily\Large\bfseries}{}{0pt}{}
\titleformat{\subsection}{\sffamily\large\bfseries}{}{0pt}{}
\titleformat{\subsubsection}{\sffamily\normalsize\bfseries}{}{0pt}{}
\setlist{itemsep=1pt,topsep=3pt}
\setlength{\parskip}{4pt}\setlength{\parindent}{0pt}
\renewcommand{\arraystretch}{1.2}
\newcolumntype{L}{>{\raggedright\arraybackslash}X}
\setlength{\emergencystretch}{3em}
\begin{document}
"""


def esc(s):
    rep = {"\\": r"\textbackslash{}", "&": r"\&", "%": r"\%", "$": r"\$", "#": r"\#", "_": r"\_",
           "{": r"\{", "}": r"\}", "~": r"\textasciitilde{}", "^": r"\textasciicircum{}"}
    return "".join(rep.get(c, c) for c in s)


def inline(s):
    parts = re.split(r"(`[^`]*`)", s)
    out = []
    for p in parts:
        if p.startswith("`") and p.endswith("`") and len(p) > 1:
            out.append(r"\texttt{" + esc(p[1:-1]).replace("/", r"/\allowbreak{}") + "}")
            continue
        p = re.sub(r"\[([^\]]+)\]\(([^)]+)\)", lambda m: "\x00L" + m.group(1) + "\x00", p)
        p = esc(p)
        p = re.sub(r"\*\*(.+?)\*\*", r"\\textbf{\1}", p)
        p = re.sub(r"(?<![\w*])\*(?!\s)(.+?)(?<!\s)\*(?![\w*])", r"\\emph{\1}", p)
        p = p.replace("\x00L", "").replace("\x00", "")
        out.append(p)
    return "".join(out)


def table(rows):
    cells = [[c.strip() for c in r.strip().strip("|").split("|")] for r in rows]
    head, body = cells[0], [r for r in cells[2:]]
    n = len(head)
    spec = "@{}" + "L" * n + "@{}"
    lines = [r"{\small\begin{tabularx}{\linewidth}{" + spec + "}", r"\toprule",
             " & ".join(r"\textbf{" + inline(h) + "}" for h in head) + r"\\", r"\midrule"]
    for r in body:
        r = (r + [""] * n)[:n]
        lines.append(" & ".join(inline(c) for c in r) + r"\\")
    lines += [r"\bottomrule", r"\end{tabularx}}", ""]
    return "\n".join(lines)


def convert(md):
    out, i, lines = [PRE], 0, md.split("\n")
    stack = []  # open list environments: (indent, env)

    def close_lists(to=0):
        while len(stack) > to:
            out.append(r"\end{" + stack.pop()[1] + "}")

    first_h1 = True
    while i < len(lines):
        ln = lines[i]
        if ln.strip() == "---":
            close_lists()
            i += 1
            continue
        m = re.match(r"^(#{1,4}) (.*)$", ln)
        if m:
            close_lists()
            lvl, txt = len(m.group(1)), inline(m.group(2))
            if lvl == 1:
                out.append(r"{\sffamily\large\bfseries " + txt + r"\par}\medskip" if first_h1 else r"\section*{" + txt + "}")
                first_h1 = False
            else:
                out.append([None, None, r"\section*{", r"\subsection*{", r"\subsubsection*{"][lvl] + txt + "}")
            i += 1
            continue
        if ln.lstrip().startswith("|"):
            close_lists()
            rows = []
            while i < len(lines) and lines[i].lstrip().startswith("|"):
                rows.append(lines[i])
                i += 1
            out.append(table(rows))
            continue
        m = re.match(r"^!\[(.*)\]\((.*)\)\s*$", ln)
        if m:
            close_lists()
            path = os.path.normpath(os.path.join("reports", m.group(2)))
            rel = os.path.relpath(path, OUTDIR)
            out.append(r"\begin{figure}[h]\centering\includegraphics[width=0.85\linewidth]{" + rel + r"}"
                       r"\caption*{\small " + inline(m.group(1)) + r"}\end{figure}")
            i += 1
            continue
        m = re.match(r"^(\s*)([-*]|\d+\.) (.*)$", ln)
        if m:
            ind = len(m.group(1))
            env = "enumerate" if m.group(2)[0].isdigit() else "itemize"
            while stack and stack[-1][0] > ind:
                out.append(r"\end{" + stack.pop()[1] + "}")
            if not stack or stack[-1][0] < ind:
                stack.append((ind, env))
                out.append(r"\begin{" + env + "}")
            text = m.group(3)
            i += 1
            while i < len(lines) and lines[i].strip() and not re.match(r"^\s*([-*]|\d+\.) ", lines[i]) \
                    and lines[i].startswith(" ") and not lines[i].lstrip().startswith("|"):
                text += " " + lines[i].strip()
                i += 1
            out.append(r"\item " + inline(text))
            continue
        if not ln.strip():
            if stack and i + 1 < len(lines) and re.match(r"^\s*([-*]|\d+\.) ", lines[i + 1] or ""):
                i += 1
                continue
            close_lists()
            out.append("")
            i += 1
            continue
        close_lists()
        para = [ln.strip()]
        i += 1
        while i < len(lines) and lines[i].strip() and not re.match(r"^(#|\||!\[|\s*([-*]|\d+\.) )", lines[i]) \
                and lines[i].strip() != "---":
            para.append(lines[i].strip())
            i += 1
        out.append(inline(" ".join(para)) + "\n")
    close_lists()
    out.append(r"\end{document}")
    return "\n".join(out)


def main():
    os.makedirs(OUTDIR, exist_ok=True)
    with open(SRC) as f:
        tex = convert(f.read())
    with open(TEX, "w") as f:
        f.write(tex)
    for _ in range(2):
        subprocess.run(["pdflatex", "-interaction=nonstopmode", "-halt-on-error", os.path.basename(TEX)],
                       cwd=OUTDIR, check=True, stdout=subprocess.DEVNULL)
    print("wrote", os.path.join(OUTDIR, "paper_draft.pdf"))


if __name__ == "__main__":
    main()
