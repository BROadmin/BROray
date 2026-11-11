# BROray 3.5.5 Implementation Plan

> For agentic workers: REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Use TDD and independent review. This existing linked worktree contains earlier release changes; do not reset, clean, or commit unrelated files.

**Goal:** Fix reported subscription imports and produce a verified four-architecture 3.5.5 candidate.
**Architecture:** Retain the existing staged subscription pipeline and server model. Add a strict Clash-to-existing-parser adapter with per-node errors. Normalize Xray client-local level explicitly.
**Tech Stack:** Go, pinned vendored YAML decoder, offline ARM64 VM, QEMU cross-architecture CLI checks, authenticated Firefox/Keenetic.
**Spec:** ../specs/2026-11-11-subscriptions-355.md

## Global constraints
Follow the spec, preserve secrets/data and first failures. No publication/reboot/opkg. Already-passed unaffected suites are not rerun. Build/sign only after all fixes and review. No frozen earlier artifacts are edited. External receipts live in G:/GPT-Codex/BROray-3.5.5.

## Review focus
| Failure | Owner | Required evidence |
|---|---|---|
| Empty range wrongly rejects or relaxes invalid bounds | T1 | URI + JSON RED/GREEN |
| Level lost silently or native disagrees | T1 | ordinary/native valid/invalid levels + warning |
| YAML unsafe expansion or silently dropped protocol options | T2 | structural bounds + per-node field refusal + output preservation |
| Whole failure hides reasons or deletes prior nodes | T1/T2 | service update error receipt + prior catalog unchanged |
| Built/deployed bytes differ; native partial import claimed complete | T3/T4 | source/archive signatures, native endpoint counts and WebUI |

## T1 — Existing import corrections
Add focused regression tests, capture RED, fix both range parsers, ordinary/native user-level normalization and failed-stage persistence. Test affected subscriptions/servers cases and race update path. Review exact diff.

## T2 — Clash import
Inspect live YAML field names in memory, verify primary format/library documentation. Add pinned vendored decoder and strict import adapter; deterministic synthetic tests before production code. Integrate format detection/staging warnings. Cover actual provider forms plus advertised supported protocols, rejection, bounds, no secret echoes. Review after GREEN.

## T3 — Candidate preparation
Update version/cache markers, protocol documentation and release notes. Run only affected packages/UI and core config checks. Freeze source, build four static archives, verify extracted CLI and signed channel using existing trusted tools. Review cumulative changes.

## T4 — Test router
Fresh read-only identity/space/data/state preflight. Deliver verified candidate in own RAM path and install via signed update. Refresh existing subscriptions corresponding to the three supplied endpoints; verify counts, protocols, warnings, UI and persistence. Preserve prior connection settings and unrelated objects. Record candid limitations; final report and candidateReady only if required evidence passes.

## T6 — Automatic alternate exports (owner follow-up)
Binding addendum: `G:/GPT-Codex/BROray-3.5.5/T6-BRIEF.md`. Discover advertised JSON/Clash exports from the primary URL, stage all sources atomically, and merge exact unique configurations under the existing subscription. Preserve provenance, distinct connection variants and previous nodes on incomplete fetch. RED/GREEN, scoped independent review, one candidate4 for four architectures and native primary-link refresh. T1–T5 accepted evidence is inherited where bytes are unchanged.
