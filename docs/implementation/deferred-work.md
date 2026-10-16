- source_spec: `docs/implementation/spec-1-1-project-skeleton-import-rules-and-ci.md`
  summary: CLAUDE.md should say `go run ./cmd/herddash version` prints `herddash dev (unknown)` and that release ldflags stamping is not wired until Story 4.3's release workflow.
  evidence: Story 1.1 review finding 15; the current text says both `(<commit>)` and `unknown`, and no workflow stamps -X main.version yet.
- source_spec: `docs/implementation/spec-1-2-configuration-data-directory-and-socket-discovery.md`
  summary: Story 2.1 should assert created database, WAL, SHM, lock and log files are 0600, which pins the umask set in main.
  evidence: Story 1.2 review; `syscall.Umask(0o077)` in main has no test because no file is created yet.
- source_spec: `docs/implementation/spec-1-2-configuration-data-directory-and-socket-discovery.md`
  summary: Story 2.1 must ensure a `database` path outside `data_dir` has an owner-only parent directory.
  evidence: Story 1.2 review; EnsureDataDir covers only data_dir, and FR-17 requires owner-only storage.
- source_spec: `docs/implementation/spec-1-3-milestone-1-findings-against-a-herdr-0-9-5-binary.md`
  summary: CLAUDE.md should warn that a plain `herdr` command run from a pane here is a 0.9.5 client talking to the 0.8.2 live server through the inherited HERDR_SOCKET_PATH, and wrap its rewritten open-questions sentence.
  evidence: Homebrew's /opt/homebrew/bin/herdr is 0.9.5 while PID 1560 runs the 0.8.2 binary (lsof, 2026-10-16); ad-hoc CLI output is evidence for neither version. Agent-context file, so deferred per triage rule.
- source_spec: `docs/implementation/spec-1-3-milestone-1-findings-against-a-herdr-0-9-5-binary.md`
  summary: Nothing enforces that internal/herdr and its gen/genwire use only the standard library.
  evidence: arch_test.go's stdlibOnly holds only core; widening it changes the arch test's permitted set, which CLAUDE.md says needs an ADR.
- source_spec: `docs/implementation/spec-1-3-milestone-1-findings-against-a-herdr-0-9-5-binary.md`
  summary: internal/engine may import internal/herdr/gen directly, so wire types can leak past the herdr package edge (AD-5, ADR-0006).
  evidence: arch_test.go maps internal/herdr/** to one node; confining gen to internal/herdr is an arch-test change needing an ADR.
- source_spec: `docs/implementation/spec-1-3-milestone-1-findings-against-a-herdr-0-9-5-binary.md`
  summary: Story 1.5 should decide whether core.NewTransition rejects a HerdrOrder on a live or backfill Transition.
  evidence: Finding Q3 makes state_change_seq snapshot-only and per server instance; the field comment says nil for live but nothing enforces it, and enforcing adds a public sentinel to core.
