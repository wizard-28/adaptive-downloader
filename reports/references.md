# Reference verification log (2026-10-12)

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

## UNRESOLVED (not cited as fact)
- "Liu et al. (smart DTN)" from the earlier related-work placeholder: **no
  matching work found**. A search for a smart data-transfer-node paper by a Liu
  returned nothing; the only "Liu" match was the DeLoad first author, so the
  placeholder was likely mislabelled. Do not cite.
- "Elastic DTN (Swargo/Arifuzzaman)": the verified Swargo/Arifuzzaman work found
  is FastBioDL (above); no separate "elastic DTN" record was found. Do not cite.
- Any "mm-link/Mahimahi" citation: Mahimahi is used for trace replay but its
  publication record was not verified in this pass; authors must add and verify
  (Netravali et al., USENIX ATC 2015 is the expected record, UNVERIFIED here).
- Cellular trace provenance: the origin of the recorded driving traces and their
  terms were not verified here; authors must supply the dataset citation.
- LinUCB / contextual bandits, Double DQN, netem: standard references not
  verified in this pass; add only after checking records.
