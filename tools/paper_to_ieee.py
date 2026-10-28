#!/usr/bin/env python3
"""Render reports/paper_draft.md in the IEEE Access template structure.

Output: reports/paper/paper_access.tex and reports/paper/paper_access.pdf. It reuses the
Markdown subset handled by tools/paper_to_pdf.py (headings, lists, pipe tables,
images, bold, italic, code, links) and adds the IEEE parts: title and author block,
abstract, keywords, numbered references from reports/references.bib (cited as
[key] in the Markdown) and wide tables spanning both columns. The Markdown file
stays the source of truth; the internal claim-to-evidence map is left out.

Section numbers stay as written in the Markdown ("7a", "9e"), because the text
refers to them, so headings are unnumbered LaTeX headings that carry the number.

The .tex follows IEEE's ieeeaccess template and is built with the official
ieeeaccess.cls unpacked in reports/ieee_access/template/.

Usage: python3 tools/paper_to_ieee.py          (IEEE Access template -> paper_access.pdf)
       python3 tools/paper_to_ieee.py --conf   (IEEE conference template -> paper_conf.pdf)
"""
import os
import re
import subprocess
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from paper_to_pdf import esc, inline as _inline  # noqa: E402

SRC = "reports/paper_draft.md"
BIB = "reports/references.bib"
OUTDIR = "reports/paper"
CONF = "--conf" in sys.argv
NAME = "paper_conf" if CONF else "paper_access"
KEYS = set(re.findall(r"^@\w+\{([^,]+),", open(BIB).read(), re.M))

TITLE = ("When Does Adapting Concurrency and Range Size Help? Adaptive Downloading versus "
         "aria2 and Fixed Settings under Non-Optimal Conditions")
KEYWORDS = ("HTTP range requests, segmented downloading, concurrency adaptation, aria2, "
            "reinforcement learning, network measurement")

PRE = r"""\documentclass{ieeeaccess}
\usepackage{cite}
\usepackage{amsmath,amssymb,amsfonts}
\usepackage{graphicx}
\usepackage{textcomp}
\usepackage[T1]{fontenc}
\usepackage[utf8]{inputenc}
\usepackage{newunicodechar,booktabs,tabularx,array,enumitem,xcolor,url}
\newunicodechar{×}{\ensuremath{\times}}
\newunicodechar{→}{\ensuremath{\rightarrow}}
\newunicodechar{−}{\ensuremath{-}}
\newunicodechar{÷}{\ensuremath{\div}}
\newunicodechar{±}{\ensuremath{\pm}}
\newunicodechar{≥}{\ensuremath{\geq}}
\newunicodechar{≤}{\ensuremath{\leq}}
\newunicodechar{ε}{\ensuremath{\varepsilon}}
\newunicodechar{…}{\ldots}
\newunicodechar{§}{\texorpdfstring{\S}{S}}
\setlist{itemsep=0pt,topsep=2pt,parsep=0pt,leftmargin=*}
\newcolumntype{L}{>{\raggedright\arraybackslash}X}
\renewcommand{\arraystretch}{1.15}
\setlength{\emergencystretch}{3em}
\hbadness=10000 \hfuzz=3pt
\urlstyle{same}
\begin{document}
\history{Manuscript prepared October 28, 2026 (draft; not submitted).}
\doi{10.1109/ACCESS.2026.DOI}
\title{%s}
\author{\uppercase{Sourajyoti Basak}\authorrefmark{1},
\uppercase{Shwetank Shrijan}\authorrefmark{1}, and
\uppercase{Ayush Konar}\authorrefmark{1}}
\address[1]{Vellore Institute of Technology, Vellore, India (Computer Networks Lab, BCSE308P)}
\markboth{Basak \headeretal: When does adapting concurrency and range size help?}
{Basak \headeretal: When does adapting concurrency and range size help?}
\corresp{Corresponding author: Sourajyoti Basak (contact details to be confirmed).}
"""


PRE_CONF = r"""\documentclass[conference]{IEEEtran}
\usepackage{cite}
\usepackage{amsmath,amssymb,amsfonts}
\usepackage{graphicx}
\usepackage{textcomp}
\usepackage{xcolor}
\usepackage[T1]{fontenc}
\usepackage[utf8]{inputenc}
\usepackage{newunicodechar,booktabs,tabularx,array,enumitem,url}
\newunicodechar{×}{\ensuremath{\times}}
\newunicodechar{→}{\ensuremath{\rightarrow}}
\newunicodechar{−}{\ensuremath{-}}
\newunicodechar{÷}{\ensuremath{\div}}
\newunicodechar{±}{\ensuremath{\pm}}
\newunicodechar{≥}{\ensuremath{\geq}}
\newunicodechar{≤}{\ensuremath{\leq}}
\newunicodechar{ε}{\ensuremath{\varepsilon}}
\newunicodechar{…}{\ldots}
\newunicodechar{§}{\S}
\setlist{itemsep=0pt,topsep=2pt,parsep=0pt,leftmargin=*}
\newcolumntype{L}{>{\raggedright\arraybackslash}X}
\renewcommand{\arraystretch}{1.15}
\setlength{\emergencystretch}{3em}
\hbadness=10000 \hfuzz=3pt
\urlstyle{same}
\begin{document}
\title{%s}
\author{\IEEEauthorblockN{Sourajyoti Basak}
\IEEEauthorblockA{\textit{Vellore Institute of Technology}\\
Vellore, India}
\and
\IEEEauthorblockN{Shwetank Shrijan}
\IEEEauthorblockA{\textit{Vellore Institute of Technology}\\
Vellore, India}
\and
\IEEEauthorblockN{Ayush Konar}
\IEEEauthorblockA{\textit{Vellore Institute of Technology}\\
Vellore, India}}
\maketitle
"""


def inline(s):
    out = _inline(s)
    # numbered citations: [key] or [key1, key2] where every key is in the bibliography
    def cite(m):
        ks = [k.strip() for k in m.group(1).split(",")]
        return r"\cite{" + ",".join(ks) + "}" if all(k in KEYS for k in ks) else m.group(0)
    out = re.sub(r"\[([A-Za-z0-9_]+(?:,\s*[A-Za-z0-9_]+)*)\]", cite, out)
    # let long identifiers and paths break inside \texttt
    out = re.sub(r"\\texttt\{([^{}]*(?:\\_[^{}]*)*)\}",
                 lambda m: r"\texttt{" + m.group(1).replace(r"\_", r"\_\allowbreak{}") + "}", out)
    return out


def table(rows):
    cells = [[c.strip() for c in r.strip().strip("|").split("|")] for r in rows]
    head, body = cells[0], cells[2:]
    n = len(head)
    wide = n >= 3
    spec = "@{}" + "L" * n + "@{}"
    env = "table*" if wide else "table"
    size = r"\scriptsize" if n >= 5 else r"\footnotesize"
    lines = [r"\begin{%s}[!t]\centering%s" % (env, size),
             r"\begin{tabularx}{%s}{%s}" % (r"\textwidth" if wide else r"\columnwidth", spec), r"\toprule",
             " & ".join(r"\textbf{" + inline(h) + "}" for h in head) + r"\\", r"\midrule"]
    for r in body:
        r = (r + [""] * n)[:n]
        lines.append(" & ".join(inline(c) for c in r) + r"\\")
    lines += [r"\bottomrule", r"\end{tabularx}", r"\end{%s}" % env, ""]
    return "\n".join(lines)


def convert(md):
    lines = md.split("\n")
    # front matter: title block (line 1 to the first "---") becomes a "Reporting conventions" note
    first_rule = next(i for i, l in enumerate(lines) if l.strip() == "---")
    front = [l for l in lines[1:first_rule] if not l.startswith("**Working title")
             and not l.startswith("*Request granularity")]
    body = lines[first_rule + 1:]
    # drop the internal claim-to-evidence map
    cut = next(i for i, l in enumerate(body) if l.startswith("## Claim"))
    body = body[:cut]
    abs_start = next(i for i, l in enumerate(body) if l.startswith("## Abstract"))
    abs_end = next(i for i in range(abs_start + 1, len(body)) if body[i].startswith("## "))
    abstract = " ".join(l.strip() for l in body[abs_start + 1:abs_end] if l.strip())
    body = body[abs_end:]
    # the extended abstract repeats the abstract at length; it stays in the Markdown only
    body = body[next(i for i in range(1, len(body)) if body[i].startswith("## ")):]
    # reporting conventions go in front of Section 3
    s3 = next(i for i, l in enumerate(body) if l.startswith("## 3."))
    body[s3:s3] = ["### 3.0 Reporting conventions", ""] + front + [""]

    if CONF:
        out = [PRE_CONF % esc(TITLE).replace(r"\_", "_"),
               r"\begin{abstract}" + inline(abstract) + r"\end{abstract}",
               r"\begin{IEEEkeywords}" + KEYWORDS + r"\end{IEEEkeywords}", ""]
    else:
        out = [PRE % esc(TITLE).replace(r"\_", "_"),
               r"\begin{abstract}" + inline(abstract) + r"\end{abstract}",
               r"\begin{keywords}" + KEYWORDS + r"\end{keywords}",
               r"\titlepgskip=-15pt", r"\maketitle", ""]
    stack, i = [], 0

    def close_lists(to=0):
        while len(stack) > to:
            out.append(r"\end{" + stack.pop()[1] + "}")

    while i < len(body):
        ln = body[i]
        if ln.strip() == "---":
            close_lists(); i += 1; continue
        m = re.match(r"^(#{2,4}) (.*)$", ln)
        if m:
            close_lists()
            lvl, txt = len(m.group(1)), inline(m.group(2))
            if lvl == 2 and txt.startswith("Extended abstract"):
                txt = "Summary of Results"
            out.append([None, None, r"\section*{", r"\subsection*{", r"\subsubsection*{"][lvl] + txt + "}")
            i += 1; continue
        if ln.lstrip().startswith("|"):
            close_lists()
            rows = []
            while i < len(body) and body[i].lstrip().startswith("|"):
                rows.append(body[i]); i += 1
            out.append(table(rows)); continue
        m = re.match(r"^!\[(.*)\]\((.*)\)\s*$", body[i])
        if m:
            close_lists()
            rel = os.path.relpath(os.path.normpath(os.path.join("reports", m.group(2))), OUTDIR)
            if CONF:
                wide = "fig8_all_controllers" in rel
                env, wd = ("figure*", r"\textwidth") if wide else ("figure", r"\columnwidth")
                out.append(r"\begin{%s}[!t]\centering\includegraphics[width=%s]{" % (env, wd) + rel + r"}"
                           r"\caption{" + inline(m.group(1)) + r"}\end{%s}" % env)
            else:
                out.append(r"\Figure[t!](topskip=0pt, botskip=0pt, midskip=0pt)[width=\columnwidth]{" + rel + r"}"
                           r"{" + inline(m.group(1)) + r"}")
            i += 1; continue
        m = re.match(r"^(\s*)([-*]|\d+\.) (.*)$", ln)
        if m:
            ind = len(m.group(1))
            env = "enumerate" if m.group(2)[0].isdigit() else "itemize"
            while stack and stack[-1][0] > ind:
                out.append(r"\end{" + stack.pop()[1] + "}")
            if not stack or stack[-1][0] < ind:
                stack.append((ind, env)); out.append(r"\begin{" + env + "}")
            text = m.group(3); i += 1
            while i < len(body) and body[i].strip() and not re.match(r"^\s*([-*]|\d+\.) ", body[i]) \
                    and body[i].startswith(" ") and not body[i].lstrip().startswith("|"):
                text += " " + body[i].strip(); i += 1
            out.append(r"\item " + inline(text)); continue
        if not ln.strip():
            if stack and i + 1 < len(body) and re.match(r"^\s*([-*]|\d+\.) ", body[i + 1] or ""):
                i += 1; continue
            close_lists(); out.append(""); i += 1; continue
        close_lists()
        para = [ln.strip()]; i += 1
        while i < len(body) and body[i].strip() and not re.match(r"^(#|\||!\[|\s*([-*]|\d+\.) )", body[i]) \
                and body[i].strip() != "---":
            para.append(body[i].strip()); i += 1
        out.append(inline(" ".join(para)) + "\n")
    close_lists()
    out += [r"\bibliographystyle{IEEEtran}", r"\bibliography{../references}"] + ([] if CONF else [r"\EOD"]) + [r"\end{document}"]
    return "\n".join(out)


def main():
    os.makedirs(OUTDIR, exist_ok=True)
    with open(SRC) as f:
        tex = convert(f.read())
    with open(os.path.join(OUTDIR, NAME + ".tex"), "w") as f:
        f.write(tex)
    if CONF:
        cls = os.path.abspath("reports/ieee_conf/template/IEEE-conference-template-062824")
        if not os.path.exists(os.path.join(cls, "IEEEtran.cls")):
            sys.exit("missing reports/ieee_conf/template/ (unzip IEEE's conference template there)")
    else:
        cls = os.path.abspath("reports/ieee_access/template")
        if not os.path.exists(os.path.join(cls, "ieeeaccess.cls")):
            sys.exit("missing reports/ieee_access/template/ieeeaccess.cls (unzip IEEE's template there)")
    env = dict(os.environ, TEXINPUTS=cls + os.pathsep + os.environ.get("TEXINPUTS", ""))
    run = lambda c: subprocess.run(c, cwd=OUTDIR, env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    tx = ["pdflatex", "-interaction=nonstopmode", NAME + ".tex"]
    run(tx); run(["bibtex", NAME]); run(tx)
    r = subprocess.run(tx, cwd=OUTDIR, env=env, stdout=subprocess.DEVNULL)
    print("wrote", os.path.join(OUTDIR, NAME + ".pdf"), "(pdflatex exit %d)" % r.returncode)


if __name__ == "__main__":
    main()
