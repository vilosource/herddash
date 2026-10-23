# ADR-0011: Tighter import rules for herdr's wire types

**Status: Accepted** on 2026-10-23, accepted in advance by the operator during Epic 5 planning.
It tightens the rules `arch_test.go` enforces; it changes no permitted edge of the spine's import
graph.

## Context

The architecture spine's AD-5 (and [ADR-0006](0006-how-herddash-speaks-to-herdr.md)) says herdr is
reached only through `internal/herdr`, that wire types live in `internal/herdr/gen`, and that they
are mapped to core types at the package edge. `arch_test.go` enforces the spine's import graph, but
it treats `internal/herdr` and every package under it as one node. So:

- `internal/engine`, which may import `internal/herdr`, could import `internal/herdr/gen` directly,
  and herdr's wire types could leak past the package edge without CI noticing.
- Only `internal/core` is held to the standard library. Nothing stops `internal/herdr`, its
  generated wire types or `genwire`, the generator, from taking a third-party dependency, although
  each is standard-library-only today and the spine records genwire as a standard-library-only
  generator.

Both were found in Story 1.3's review and deferred, because changing what `arch_test.go` permits
is a structural change and needs an ADR.

## Decision

- **The wire types are confined to `internal/herdr`.** `internal/herdr/gen` and every package under
  it, including `internal/herdr/gen/genwire`, may be imported only by non-test Go files in
  `internal/herdr` or a package under it. Any other import of them is a violation.
- **`internal/herdr` is standard-library-only.** `internal/herdr` and every package under it may
  import only the standard library, apart from the module's own packages, which stay governed by
  the existing edge rules (`herdr -> core`, and imports within `internal/herdr`).
- **`arch_test.go` enforces both**, and its fixture tree breaks each rule at least once so a checker that
  stops enforcing one fails.
- `_test.go` files stay exempt, as they are from every rule in `arch_test.go`.
- Nothing else changes: no permitted edge is added or removed.

## Consequences

- A wire type can reach the engine only through `internal/herdr`'s mapping to core types, so a
  change to herdr's protocol stays inside one package, as AD-5 intends.
- A third-party dependency in the herdr client or its generator now fails CI, and would need its
  own ADR, as any new dependency already does.
- `internal/herdr` imports nothing outside the standard library today, so the rule needs no
  exception and changes no code.

## Alternatives considered and rejected

- **Make `internal/herdr/gen` a node of its own** in the permitted graph. It would express the
  same confinement, but changes the spine's diagram and the meaning of a node, for one rule.
- **Move the wire types under `internal/herdr/internal/`** and let the Go toolchain enforce the
  confinement. It enforces the same rule at build time, but moves generated code and the
  `go generate` target for no gain over a check CI already runs.
- **Leave both to review.** Both were missed until Story 1.3's review found them; a test does not
  forget.
