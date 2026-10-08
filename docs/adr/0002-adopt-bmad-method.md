# ADR-0002: Adopt the BMAD Method, and where its records live

**Status: Accepted.** Written 2026-10-07, accepted 2026-10-08. It changes no code, only how planning and
delivery documents are produced and where they are kept.

## Context

This repository already has a planning record: a PRD, numbered ADRs in Nygard format, and frozen
research notes, all under `docs/`. CONTRIBUTING.md makes the ADRs the contract for structural
change. ADR-0001 is Proposed and blocks the build.

The operator wants to run the project with the BMAD Method (version 6.12.1, BMM module), which
brings its own planning chain: brief or PRD, architecture, epics and stories, sprint status, then
build and review, each produced by a skill that reads and writes documents at configured paths.
Installed as shipped, those paths point at a `_bmad-output/` folder beside `docs/`, and the
architecture skill records decisions as `AD-n` entries in a spine document rather than as ADRs.

Three overlaps follow, and each would produce two live documents saying different things if left
to default:

1. **Two PRDs.** `docs/prd.md` exists. The BMAD PRD skill writes `prd.md` into its own run folder.
2. **Two decision records.** The architecture spine's `AD-n` entries and `docs/adr/` would both
   claim to be where structural decisions are made.
3. **Two document trees.** `docs/` is the repository's single entry point for a fresh session, and
   CLAUDE.md is built around that. A second tree under `_bmad-output/` splits it.

BMAD's own override mechanism, `_bmad/custom/config.toml`, is committed, deep-merged over the
installer's config, and never touched by a re-install. It is the intended place to pin paths.

## Decision

**Adopt BMAD, and bend it to the repository's existing record rather than the other way round.**

- **Everything BMAD produces lives under `docs/`.** `_bmad/custom/config.toml` pins the output
  folder to `docs`, planning artifacts to `docs/planning/` and implementation artifacts to
  `docs/implementation/`. Project knowledge already pointed at `docs`. The `_bmad-output/` folder
  is not used and not created.
- **Those outputs are committed**, run folders and memlogs included. They are the working record,
  and BMAD's own routing reads them to know what is done.
- **The ADRs stay the authority on structural decisions**, exactly as CONTRIBUTING.md defines the
  threshold: a new dependency, a change to how herdr is spoken to, a change to the stored event
  schema, or a new network surface. The architecture spine is a build substrate. Any `AD-n` that
  meets the threshold gets a numbered ADR, and the spine cites the ADR number rather than
  restating the decision. Decisions below the threshold live in the spine alone.
- **`docs/prd.md` is the input to the BMAD PRD skill in update mode, not a competitor to its
  output.** When that run finalizes, the finalized document replaces `docs/prd.md` in the same
  pull request, so there is one PRD and it is where CLAUDE.md says it is.
- **ADR-0001 is decided before the PRD is reworked.** BMAD's chain starts from requirements, and
  the PRD's sections 7 to 12 depend on which option ADR-0001 accepts. Running the PRD skill first
  would rewrite them against an undecided premise.
- **Agent instructions stay in CLAUDE.md** for now. BMAD's project-context skill manages a block in
  `AGENTS.md`; adopting it is a separate, later choice, and until then nothing is duplicated
  between the two files.

## Consequences

- The next steps are fixed in order: accept or overturn ADR-0001, then the BMAD PRD skill in update
  mode against `docs/prd.md`, then architecture, then epics and stories, then sprint planning. The
  `bmad-help` skill will recommend the same order once it sees no finalized PRD under
  `docs/planning/`.
- A re-run of the BMAD installer cannot move the outputs, because the custom config wins the merge.
  It will recreate an empty `_bmad-output/` directory, which git ignores because it is empty.
- The 29 generated skills under `.claude/skills/` and the `_bmad/` tree are committed, which is
  what makes the method available to every agent launcher on a machine without per-machine setup.
  Upgrades of BMAD are deliberate, by re-running the installer, and land as `chore:` commits.
- BMAD's skills depend on `uv` to run their Python helpers. That is a toolchain requirement for
  anyone running the planning skills, not for building herddash, and README should not list it
  as a build requirement.
- Commits and pull requests produced through BMAD's build and review skills remain subject to the
  no-AI-attribution rule and to Conventional Commits. BMAD does not add trailers, but nothing in
  it enforces the rule either; the reviewer does.

## Alternatives considered and rejected

- **Keep the default `_bmad-output/` tree.** Rejected: it splits the single entry point that
  CLAUDE.md and the fresh-session commit were built to provide, and the routing skill would never
  see the existing PRD.
- **Retire the ADRs in favour of the architecture spine.** Rejected: the spine is distilled from a
  run's memlog and re-distilled on update, so it is not immutable, and CONTRIBUTING.md's contract
  with outside contributors is written in terms of ADRs.
- **Move `docs/prd.md` into a BMAD run folder by hand to pre-empt the overlap.** Rejected: the
  PRD skill's update mode exists for exactly this input, and moving the file before ADR-0001 is
  decided would only relocate a document that is about to be rewritten.
- **Do not adopt BMAD; keep the PRD-plus-ADR flow alone.** Rejected by the operator: the method's
  value here is the gated chain from requirements to stories to build, which the current flow
  leaves to habit.
