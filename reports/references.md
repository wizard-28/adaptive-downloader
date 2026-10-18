# Reference verification log (2026-10-12; additions 2026-10-18)

Method: each entry was checked against an authoritative record fetched on the
date above (Crossref API record, IETF/RFC Editor page, arXiv abstract page,
NSF-PAR record, or the aria2 project's own manual). No entry was written from
memory. `reports/references.bib` contains only the VERIFIED entries.

| Key | Status | Source checked | Notes |
|---|---|---|---|
| spiteri2016bola | VERIFIED | Crossref DOI 10.1109/infocom.2016.7524428 | INFOCOM 2016, pp. 1-9 |
| spiteri2020bola | VERIFIED | Crossref DOI 10.1109/tnet.2020.2996964 | IEEE/ACM ToN 28(4):1698-1711 |
| arifuzzaman2023falcon | VERIFIED | Crossref DOI 10.1109/tpds.2023.3282872 | IEEE TPDS 34(8):2265-2278 |
| arifuzzaman2021online | VERIFIED | Crossref DOI 10.1145/3458817.3476208 | SC '21 |
| balaprakash2016direct | VERIFIED (4 of 5 authors from Crossref) | Crossref DOI 10.1109/icpp.2016.36 | fifth author from search record only |
| abdollah2025mdtp | VERIFIED (record), venue name partly inferred | NSF-PAR record; DOI 10.1109/ICCCN65249.2025.11133911 | confirm full venue title before submission |
| swargo2025fastbiodl | VERIFIED as arXiv preprint | arXiv:2508.05511 | no peer-reviewed venue found; cite as preprint |
| liu2025deload | VERIFIED as arXiv preprint | arXiv:2510.18459 | no peer-reviewed venue found; cite as preprint |
| rfc9110 | VERIFIED | rfc-editor.org/info/rfc9110 | STD 97, June 2022 (month from search record) |
| aria2manual | VERIFIED | aria2.github.io manual | used for option semantics only |
| netravali2015mahimahi | VERIFIED (2026-10-18) | USENIX ATC '15 page citation metadata | pp. 417-429; ISBN 978-1-931971-225 |
| winstein2013sprout | VERIFIED (2026-10-18) | USENIX NSDI '13 page citation metadata | pp. 459-471; provenance of the five replayed traces: `mahimahi/traces/README` (repo commit f1346c3) states they were recorded with Saturator from this paper |
| li2010linucb | VERIFIED (2026-10-18) | Crossref DOI 10.1145/1772690.1772758 | WWW 2010, pp. 661-670 |
| mnih2015dqn | VERIFIED (2026-10-18) | Crossref DOI 10.1038/nature14236 | Nature 518(7540):529-533 |
| vanhasselt2016ddqn | VERIFIED (2026-10-18) | Crossref DOI 10.1609/aaai.v30i1.10295 | AAAI 30(1) |
| tcnetem | VERIFIED (2026-10-18) | man7.org tc-netem(8) page (HTTP 200) | software manual; iproute2 6.1.0 installed |

## UNRESOLVED (not cited as fact)
- "Liu et al. (smart DTN)" from the earlier related-work placeholder: **no
  matching work found**. A search for a smart data-transfer-node paper by a Liu
  returned nothing; the only "Liu" match was the DeLoad first author, so the
  placeholder was likely mislabelled. Do not cite.
- "Elastic DTN (Swargo/Arifuzzaman)": the verified Swargo/Arifuzzaman work found
  is FastBioDL (above); no separate "elastic DTN" record was found. Do not cite.
- ~~Mahimahi~~, ~~trace provenance~~, ~~LinUCB / DQN / Double DQN~~, ~~netem~~:
  resolved 2026-10-18 (table above).
- **Still open (human):** the *redistribution terms* of the Mahimahi-bundled
  traces. The Mahimahi repository is GPL-3.0 (`COPYING`), and the traces
  README states their origin but gives no separate data licence. Confirm
  whether redistributing them in a data archive is permitted; citing them is
  unaffected.
