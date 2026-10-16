- source_spec: `docs/implementation/spec-1-1-project-skeleton-import-rules-and-ci.md`
  summary: CLAUDE.md should say `go run ./cmd/herddash version` prints `herddash dev (unknown)` and that release ldflags stamping is not wired until Story 4.3's release workflow.
  evidence: Story 1.1 review finding 15; the current text says both `(<commit>)` and `unknown`, and no workflow stamps -X main.version yet.
- source_spec: `docs/implementation/spec-1-2-configuration-data-directory-and-socket-discovery.md`
  summary: Story 2.1 should assert created database, WAL, SHM, lock and log files are 0600, which pins the umask set in main.
  evidence: Story 1.2 review; `syscall.Umask(0o077)` in main has no test because no file is created yet.
- source_spec: `docs/implementation/spec-1-2-configuration-data-directory-and-socket-discovery.md`
  summary: Story 2.1 must ensure a `database` path outside `data_dir` has an owner-only parent directory.
  evidence: Story 1.2 review; EnsureDataDir covers only data_dir, and FR-17 requires owner-only storage.
