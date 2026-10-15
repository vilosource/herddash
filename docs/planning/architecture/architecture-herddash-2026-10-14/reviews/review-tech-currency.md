# Review: technology currency and reality check

**Target:** `ARCHITECTURE-SPINE.md` (this folder), `docs/adr/0004` to `0007`, at commit `3dd14fc`.
**Lens:** was every committed decision checked against the web, the project or a real build, rather
than asserted from memory? Does each named technology still exist, fit, and behave as claimed?
**Reviewed:** 2026-10-15.

## Verdict

**Pass with fixes.** The versions pinned in the memlog are all current today, and the stack works:
a probe built from the exact pins cross-compiles with `CGO_ENABLED=0` for all four targets and
serves the spine's routes. The fixes are about what surrounds the versions. ADR-0004 calls a
dependency upgrade routine when the SQLite driver's documentation says it is not. herdr shipped
0.9.5 the day before the spine's update, and that release fixes a status bug that affects
herddash's supported range. ADR-0006 states several protocol claims as plain fact when the research
record marks them UNVERIFIED. The memlog says the project docs were moved to Go 1.26, but they were
not.

## What was checked, and how

Everything below was fetched or run on 2026-10-15. None of it comes from memory.

| Claim | Source checked | Result |
| --- | --- | --- |
| Go stable 1.27.2 and 1.26.9 | `go.dev/dl/?mode=json` | **Confirmed.** These are the only two stable entries. |
| `modernc.org/sqlite` v1.60.1 is latest | `proxy.golang.org/modernc.org/sqlite/@latest` | **Confirmed**, published 2026-09-29. |
| `github.com/BurntSushi/toml` v1.6.0 is latest | `proxy.golang.org/.../@latest`, GitHub API | **Confirmed**, published 2025-12-18. The repository is active (last push 2026-08-18) and not archived. |
| herdr latest v0.9.4, at `herdrdev/herdr` | GitHub API, releases | **Stale.** v0.9.5 shipped 2026-10-14T16:07Z. The repository is `herdrdev/herdr`. |
| sqlite v1.60.1 fits a `go 1.26` module | the v1.60.1 `go.mod` | **Fits, with no headroom.** Its `go.mod` says `go 1.26.0`. The v1.60.0 changelog says "Go 1.26 is now required". |
| sqlite supports darwin/arm64, darwin/amd64, linux/amd64, linux/arm64 | GitLab tree `lib/` at v1.60.1 | **Confirmed.** `sqlite_{darwin,linux}_{amd64,arm64}.go` all exist, and CONTRIBUTING.md says 20 targets are supported. |
| `CGO_ENABLED=0` cross-compiles all four (ADR-0004) | probe module built with Go 1.26.5 | **Confirmed.** All four built as static binaries of about 12 MB. darwin/arm64 ran SQLite 3.53.4 with WAL enabled and decoded TOML. |
| Go 1.22+ `ServeMux` handles `GET /api/agents/{host}/{server}/{pane}/history` | `httptest` probe | **Confirmed.** Three side effects are noted under F-6. |
| `log/slog` suits JSON logging | Go 1.26 and 1.27 release notes | **Fine.** Go 1.26 adds `slog.NewMultiHandler`. Nothing was removed or changed. |
| Go 1.26/1.27 changes to defaults the spine relies on | `go.dev/doc/go1.26`, `go.dev/doc/go1.27` | Two matter: macOS floor and Unix-socket EOF wrapping. See F-5. |

The probe lives in this session's scratchpad. It was not added to the repository.

## Findings

### F-1 [High] ADR-0004 says a dependency upgrade is ordinary. For this driver it is not.

ADR-0004, Consequences: "An upgrade is an ordinary dependency bump, not an ADR, unless it changes
the on-disk format."

`modernc.org/sqlite`'s package documentation has a section titled **"Fragile modernc.org/libc
dependency"**: "you should use in your go.mod file the exact same version of modernc.org/libc as
seen in the go.mod file of this repository" (gitlab.com/cznic/sqlite issue #177). The changelog
repeats it for every release from v1.58.0 to v1.60.0. v1.60.1 pins `modernc.org/libc v1.77.1`.
`proxy.golang.org` already serves **libc v1.78.0** (2026-10-11). A routine `go get -u ./...` or a
Dependabot bump of libc alone produces an unsupported combination.

That combination can fail at run time rather than at compile time. The maintainers' SECURITY.md
also says **only the latest release is supported**, so the pin cannot be left alone for long either.

The driver also controls herddash's minimum Go version. v1.60.0 raised its requirement to Go 1.26
without notice, and the next raise will force herddash's `go` directive up whether or not anyone
decides to.

**Fix:** amend ADR-0004's Consequences:

- `modernc.org/libc` is pinned to exactly the version in the driver's `go.mod`, and it moves only
  together with the driver.
- No blanket `go get -u`.
- A CI check compares the two pins.
- The `go` directive follows the driver's minimum.

### F-2 [Medium] herdr is at 0.9.5, and the fix it carries affects herddash's supported range

Spine §Stack and ADR-0006 Context say "latest 0.9.4 (2026-10-13)". **v0.9.5 shipped on 2026-10-14.**
Its notes include:

- "**Claude Code questions show as blocked in named sessions.** (#4573, #5192)". Before 0.9.5,
  a Claude Code pane in a named session that was waiting on a question was not reported `blocked`.
  The spine supports named sessions (AD-3: `server = <name>` for `sessions/<name>/herdr.sock`) and
  takes `needs_you` from `blocked`. So across the declared supported range of 0.9.2 to 0.9.4, the
  Board would miss exactly the Agent it exists to surface whenever the pane is in a named session.
- "Codex status comes from screen detection again". This bears on the deferred Codex Profile rules.

Also relevant from 0.9.4: "Antigravity and Grok count as idle once they are back at their prompt".
This changes when Antigravity reaches `idle` or `done`, which the deferred Antigravity rules depend
on.

**Fix:**

- Update the Stack row and ADR-0006 to 0.9.5.
- Either raise the floor for full support to 0.9.5, or have FR-16's version gate flag `blocked` as
  unreliable for named sessions below 0.9.5.
- Record the per-version detection changes where the Profile rules are deferred.

### F-3 [Medium] ADR-0006 and AD-5 state UNVERIFIED protocol claims as fact

`docs/research/herdr-socket-api.md` §Provenance marks seven items UNVERIFIED. ADR-0006's status line
says "Items that record marks UNVERIFIED stay unverified here". The body then states them without
any marker. AD-5 in the spine restates them with no marker and no pointer.

| Statement as written | Research status |
| --- | --- |
| ADR-0006 Context: "Subscriptions replay nothing." | UNVERIFIED: the 0.9.0 move to live-only delivery |
| ADR-0006 Context: "A lost-events condition closes the stream." | UNVERIFIED: `events_lost`, its shape and its recovery |
| ADR-0006 Context: "`pane.focus` and `agent.focus` mutate herdr's seen state" | UNVERIFIED: the 0.9.3 seen-state semantics |
| ADR-0006 Context: "an unbounded output read drives an agent's own scroll interface" | UNVERIFIED, and deliberately never tested |
| ADR-0006 Decision, Resync: "a rejected subscribe triggers a resync" | rests on "one bad `pane_id` rejects the whole request", UNVERIFIED |
| ADR-0006 Consequences: "about 15 connections. That is acceptable, and **it is the only design the protocol allows**" | rests on one request per connection, confirmed only on 0.8.2 and the research's **first open item** for 0.9.x, and on "no concurrent-connection cap", UNVERIFIED |
| ADR-0006 Consequences: "against 0.8.2 ... subscribing replays stale events" | inferred from the UNVERIFIED 0.9.0 change |
| Spine AD-5: "subscribes before snapshotting, treats the snapshot as truth and events as invalidation" | the ordering and the recovery procedure are UNVERIFIED |

For the never-focus and never-read-output rules this changes nothing in practice: the research
explains that complying costs nothing either way. It matters for the connection model, because "the
only design the protocol allows" is the claim most likely to be revisited in milestone 1.

Part of this is now corroborated from outside. The **herdr v0.9.2 release notes** (GitHub) say "A
reader that falls too far behind now gets an `events_lost` error instead of silently skipping
events" (#4178, #4225) and "Socket error responses keep the original request ID, including
subscription setup errors" (#4344). That is release-note evidence, not first-hand, but it is
stronger than the research had.

**Fix:**

- Mark each of the claims above inline in ADR-0006, for example "(UNVERIFIED, research §Provenance)".
- Change "the only design the protocol allows" to "the design the protocol requires if
  one-request-per-connection holds on 0.9.x, milestone 1's first check".
- Add the same pointer to AD-5.
- Optionally cite the 0.9.2 release notes for `events_lost`.

### F-4 [Medium] The memlog says the Go minimum was updated. The project docs still say 1.25.

Memlog: "go.mod requires go 1.26 ... CLAUDE.md and CONTRIBUTING updated from 1.25." At `3dd14fc`
they were not:

- `CLAUDE.md:135`: "Requires Go 1.25 or newer"
- `CONTRIBUTING.md:54`: "Requires **Go >= 1.25**"
- `README.md:38`: "Go 1.25 or newer"

Go 1.25 cannot satisfy a module that requires `modernc.org/sqlite` v1.60.x. With the default
`GOTOOLCHAIN=auto` it silently downloads 1.26. With `GOTOOLCHAIN=local` it fails. Go 1.25 is also
outside the support window now, as the memlog itself records.

**Fix:** update all three files in this change, or remove the memlog's claim.

### F-5 [Low] Two Go changes the spine does not mention

**Go 1.27 requires macOS 13 Ventura or later.** This is in the 1.27 release notes and was announced
in 1.26. Releases built with 1.27.2 will not run on macOS 12 or earlier. The Deployment note lists
darwin/amd64, the target most likely to be an old Intel Mac, and states no macOS floor. Add one, or
build releases with 1.26.x.

**Go 1.27 changes how `net.UnixConn` reports end of file.** Reads now return `io.EOF` directly
instead of wrapping it in `*net.OpError`. The `go` directive does not gate this, so code that checks
`err == io.EOF` behaves differently when tested on 1.26 and released on 1.27. `internal/herdr`
reads EOF on every connection, one request each, and on every subscription close. Add a convention:
`errors.Is(err, io.EOF)`, never `==`.

**Informational:**

- This machine's local toolchain is **1.26.5**. The current patch release is 1.26.9.
- Under Go 1.27, `go mod init` writes `go 1.26.0`, and `go get` normalises the directive to
  `go 1.26.0`. Both agree with the spine.

### F-6 [Low] The Go 1.22+ routing works, with three side effects to pin down

Probe results:

- **Path values decode correctly.** `/api/agents/mbp.local/default/w1:p1/history` gives
  `mbp.local|default|w1:p1`, and `wB%3Ap7` decodes to `wB:p7`. A pane id containing a colon is fine.
- **`%2F` decodes into a single segment value.** `/api/agents/mbp/a%2Fb/...` gives
  server=`a/b`. Validate `host` and `server` against their allowed character sets rather than
  trusting the router.
- **`GET /` matches every path.** `GET /nope` returned 200 from the `/` handler. Use `GET /{$}` for
  the Board root, plus explicit asset routes, or unknown paths will serve the Board with 200 instead
  of the JSON 404 the convention calls for.
- **"GET only" means GET and HEAD.** A `GET` pattern also matches `HEAD`, which is harmless. A
  mismatched method gets a **plain-text** 405 with `Allow: GET, HEAD`, not the
  `{"error":{...}}` body the JSON convention promises. Either accept that for 405, or route a
  method-agnostic fallback.

**SSE:** the standard library supports it, through `http.Flusher` or `http.ResponseController`.
Note that a `Server.WriteTimeout` cuts off long-lived `/events` streams. Clear the deadline for that
route with `ResponseController.SetWriteDeadline(time.Time{})`.

### F-7 [Medium, beyond the strict lens] ADR-0007's "no CORS headers" does not stop a cross-origin page reading the API

ADR-0007: "No CORS headers, so a page from another origin cannot read the API from a browser." That
holds only while the page's origin is not loopback. A **DNS-rebinding** attack points an
attacker-controlled hostname at `127.0.0.1`. The browser then treats the API as same-origin and
reads `/api/state` and history, which hold pane titles, working directories and Profiles.

The usual defence is to reject any request whose `Host` header is not `127.0.0.1:<port>`,
`localhost:<port>` or `[::1]:<port>`. Go 1.25's `http.CrossOriginProtection` does not help, because
it guards only non-safe methods and every route here is GET.

**Fix:** add a Host-header allowlist to ADR-0007, and correct the sentence.

### F-8 [Info] Smaller points

- **TOML 1.1.** BurntSushi/toml v1.6.0 "enables TOML 1.1 by default". ADR-0004 says the format
  "matches herdr's own `config.toml`". That is true in direction, but herddash will accept 1.1
  syntax that other TOML 1.0 tooling may reject. This is harmless unless the provisioning repository
  templates or lints the file.
- **The rejection of ncruces.** ADR-0004 rejects `ncruces/go-sqlite3` for its "smaller user base".
  The driver is actively maintained (push 2026-10-14), but it is still **v0.35.6**, pre-1.0. That is
  a firmer and checkable reason than user base, and it parallels ADR-0006's reason for rejecting the
  v0 herdr clients.
- **sqlite usage notes worth carrying into `internal/store` stories**, from the v1.60.x
  documentation:
  - Bound the pool with `SetMaxOpenConns`. `database/sql` is unbounded by default, and each
    connection carries its own page cache and libc thread state.
  - A plain filename DSN has its `?query` stripped, so `/path/db?mode=ro` opens read-write. Use a
    `file:` URI.
  - Consider `StrictPragmas`.
  - AD-9's separate lock file avoids the POSIX-lock pitfall that the driver's OFD-locking notes
    describe.
- **The wire-type generator.** ADR-0006 says wire types are generated from
  `herdr api schema --json`, but names no generator. If it is a third-party tool, it is a build-time
  dependency that ADR-0004's "no other runtime dependency" clause does not cover. Name it, or state
  that the generator is hand-written.
- **The repository "moved".** The memlog says herdr's "repo moved to herdrdev/herdr", but the
  research record of 2026-10-05 already cites `herdrdev/herdr`. Nothing needs changing. The note is
  just inaccurate.

## Summary

| # | Severity | Location | Issue |
| --- | --- | --- | --- |
| F-1 | High | ADR-0004 | libc must be pinned to the driver's exact version; upgrades are not ordinary |
| F-2 | Medium | Spine §Stack, ADR-0006 | herdr 0.9.5 exists and fixes `blocked` in named sessions |
| F-3 | Medium | ADR-0006, Spine AD-5 | UNVERIFIED protocol claims stated as fact; "only design the protocol allows" overclaims |
| F-4 | Medium | CLAUDE.md, CONTRIBUTING.md, README.md vs memlog | Go minimum still 1.25 despite the memlog |
| F-5 | Low | Spine Deployment, Conventions | Go 1.27 macOS 13 floor; Unix socket EOF change |
| F-6 | Low | Spine AD-6, Conventions | `GET /` catch-all, `%2F` in segments, plain-text 405, SSE write deadline |
| F-7 | Medium | ADR-0007 | DNS rebinding defeats the no-CORS reasoning; needs a Host allowlist |
| F-8 | Info | ADR-0004, ADR-0006, memlog | TOML 1.1, ncruces is v0, sqlite pool and DSN notes, unnamed generator, "moved" note |
