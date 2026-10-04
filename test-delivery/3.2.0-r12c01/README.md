# BROray 3.2.0-r12c01 — TEST-ONLY manual delivery

This branch is intentionally separate from `main` and Stable.

- Candidate: `3.2.0-r12c01`
- Application SHA-256: `c8fc8b5478178bf9eb0f07db1f67453722a32a940b664ce91ed448029829399f`
- Original installer SHA-256: `708ef201c6f8eab48fbb0896a7b464a64e16227b201bd5762b196e4574bdb35c`
- Purpose: controlled physical validation only.
- The public r12 release was withdrawn after a field defect. Do not treat this branch as Stable.
- The wrapper installs current Stable 3.1.1-r12 first only when BROray is absent, then runs the exact r12c01 installer using a local fetch hook for withdrawn r12 assets.
- No Stable or main-branch files are modified by this delivery branch.
