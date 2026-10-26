# Installing herddash

herddash runs as your own user and starts with your session: a launchd agent on macOS, a systemd
user unit on Linux. Nothing here needs root, and nothing in this repository installs anything for
you. These are the steps to do it by hand; the provisioning repository does the same on the
operator's machines.

Two rules hold throughout:

- **Never touch herdr's configuration or its plugin registry.** Nothing below edits
  `~/.config/herdr/`. herddash only reads herdr's socket.
- **herddash is never installed as a herdr plugin.** It is a separate program with its own
  service unit.

herddash must run as the same user as herdr, because herdr's socket is readable only by its owner.

Every command below runs from a clone of this repository unless it says otherwise.

## 1. Put the binary in `~/.local/bin`

Both units start `~/.local/bin/herddash`, so the binary goes there.

Either download a release binary or build one.

**Download a release binary.** Each release on
<https://github.com/vilosource/herddash/releases> carries one binary per platform, built with
`CGO_ENABLED=0` so it needs no Go or C toolchain on the Host, named `herddash_<version>_<os>_<arch>`
(the version without its `v`), and a `SHA256SUMS` file covering all four:

| Platform | Asset |
|---|---|
| macOS, Apple silicon | `herddash_<version>_darwin_arm64` |
| macOS, Intel | `herddash_<version>_darwin_amd64` |
| Linux, x86-64 | `herddash_<version>_linux_amd64` |
| Linux, ARM64 | `herddash_<version>_linux_arm64` |

The macOS binaries need macOS 13 or newer. They are not signed or notarised. For example, 0.1.0
on Apple silicon (set `ASSET` to your platform's name from the table; on Linux, use
`sha256sum -c -` in place of `shasum -a 256 -c -`):

```bash
V=0.1.0
ASSET=herddash_${V}_darwin_arm64
DL=$(mktemp -d)
cd "$DL"
curl -fLO "https://github.com/vilosource/herddash/releases/download/v$V/$ASSET"
curl -fLO "https://github.com/vilosource/herddash/releases/download/v$V/SHA256SUMS"
grep " $ASSET\$" SHA256SUMS | shasum -a 256 -c - &&
  mkdir -p ~/.local/bin &&
  install -m 755 "$ASSET" ~/.local/bin/herddash
cd - >/dev/null
```

With `gh`, `gh release download "v$V" --repo vilosource/herddash --pattern "$ASSET" --pattern
SHA256SUMS` replaces the two `curl` lines.

The check must print `herddash_<version>_<os>_<arch>: OK`. It fails if `SHA256SUMS` has no line
for the asset or the hash differs, and then the `&&` chain installs nothing. A file downloaded
through a browser rather than `curl` or `gh` is quarantined on macOS; clear that with
`xattr -d com.apple.quarantine ~/.local/bin/herddash`.

A release with no binaries attached (the release workflow missed it, for example because a later
release-please run created it) is recovered by running the release workflow by hand:
`gh workflow run release.yml --repo vilosource/herddash -f tag=vX.Y.Z`. It refuses a release that
already has assets; add `-f force=true` to replace them.

**Or build it** (Go 1.26 or newer):

```bash
mkdir -p ~/.local/bin
go build -o ~/.local/bin/herddash ./cmd/herddash
```

`scripts/release-build.sh <version> <commit> <outdir>` builds the four release binaries and their
`SHA256SUMS` the same way a release does; a release builds them with Go 1.27.2.

Check it runs:

```bash
~/.local/bin/herddash version
```

## 2. Write the configuration

herddash reads `~/.config/herddash/config.toml` at start and runs fine without it. Start from the
documented example:

```bash
mkdir -p ~/.config/herddash
cp docs/config.example.toml ~/.config/herddash/config.toml
```

Edit it, then check what herddash will actually use:

```bash
~/.local/bin/herddash config
```

That prints the effective configuration as JSON, or names the key that is wrong. Run it from a
plain shell, not from inside a herdr pane: a pane exports `HERDR_SOCKET_PATH`, which the service
does not have.

**Which herdr to watch.** The units set no `HERDR_SOCKET_PATH` and no `HERDR_SESSION`, so under a
service herddash finds the socket by herdr's own order and ends at the default
`~/.config/herdr/herdr.sock`. If your herdr runs as a named session or on another socket, pin it
in the config file with `session` or `socket_path`; do not add environment variables to the unit.

**Where the data lives.** The data directory and database are the `data_dir` and `database` that
`herddash config` prints. By default the data directory is `$XDG_DATA_HOME/herddash`, else
`~/.local/share/herddash`, and the database is `herddash.db` in it. They move with `data_dir` and
`database` in the config, and with `XDG_DATA_HOME` in the service's own environment, which is not
your shell's:

- launchd passes no `XDG_DATA_HOME`, so on macOS the service uses `~/.local/share/herddash`.
- A systemd user manager may carry one; see it with
  `systemctl --user show-environment | grep XDG_DATA_HOME`.

To take the doubt away, set `data_dir` in the config. The steps below read both paths from
`herddash config` rather than assuming them.

Configuration is read only at start. After editing it, restart the service (below).

## 3a. macOS: the launchd agent

The template is `deploy/launchd/io.github.vilosource.herddash.plist`. launchd expands neither
`~` nor variables, so the template says `__HOME__` wherever your home directory belongs, and
installing it is one `sed` line.

The standard-error log path is fixed in the plist at
`~/.local/share/herddash/herddash.stderr.log`, whatever `data_dir` says, unless you edit the plist.

**Check the launchd environment.** A launchd agent inherits whatever `launchctl setenv` has set
for your session. This must print nothing:

```bash
launchctl getenv HERDR_SOCKET_PATH; launchctl getenv HERDR_SESSION
```

If either prints a value, something has exported it into launchd; `launchctl unsetenv` it and find
what set it.

**Install and start.** Create the standard-error log's directory first, owner-only, because
launchd opens that log before herddash starts and herddash refuses a data directory others can
read. `mkdir -m` does not change a directory that already exists, so `chmod` it as well:

```bash
mkdir -p ~/.local/share
mkdir -m 700 -p ~/.local/share/herddash && chmod 700 ~/.local/share/herddash
mkdir -p ~/Library/LaunchAgents
sed "s|__HOME__|$HOME|g" deploy/launchd/io.github.vilosource.herddash.plist \
  > ~/Library/LaunchAgents/io.github.vilosource.herddash.plist
grep -F "<string>$HOME/.local/bin/herddash</string>" \
  ~/Library/LaunchAgents/io.github.vilosource.herddash.plist && echo "plist OK"
plutil -lint ~/Library/LaunchAgents/io.github.vilosource.herddash.plist
```

If `grep` prints nothing, the substitution failed, which happens when `$HOME` contains `|` or `&`;
edit the installed plist by hand. Then load it. `bootstrap` fails with error 5 if the agent is
already loaded, so unload any earlier copy first. `bootout` returns before the process has
exited, so wait for it. The wait is on any `herddash` process of yours, so stop one you started by
hand (a `-dev` build, `serve` in a terminal, or a running `import-plugin-log`) first, or the loop
never ends; every launchd block below waits the same way:

```bash
launchctl bootout gui/$(id -u)/io.github.vilosource.herddash 2>/dev/null
while pgrep -x -u "$(id -u)" herddash >/dev/null; do sleep 1; done
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/io.github.vilosource.herddash.plist
```

From now on it starts at every login (`RunAtLoad`).

**Check it.**

```bash
launchctl print gui/$(id -u)/io.github.vilosource.herddash | grep -E 'state|pid|last exit'
```

`state = running` with a `pid` means it is up. Open the Board (step 4).

**Restart it**, after a config change or a new binary:

```bash
launchctl kickstart -k gui/$(id -u)/io.github.vilosource.herddash
```

After a new binary, expect the Board to be down for about 30 seconds. The binaries are only
ad-hoc signed, and launchd refuses the first start of a changed one: `last exit reason =
OS_REASON_CODESIGNING`, with `Launch Constraint Violation` in the system log. It then holds off
its 30-second respawn throttle, records the new binary and starts it. Nothing needs doing; wait
for `state = running` before deciding it failed:

```bash
until launchctl print gui/$(id -u)/io.github.vilosource.herddash | grep -q 'state = running'; do sleep 2; done
```

*Seen on macOS 26.5.1 on 2026-10-26, at each of three binary changes, from `/usr/bin/log show`
(launchd: "Requesting LWCR update on next spawn", then running 30 s later).*

**Stop it.**

```bash
launchctl bootout gui/$(id -u)/io.github.vilosource.herddash
while pgrep -x -u "$(id -u)" herddash >/dev/null; do sleep 1; done
```

It stays stopped until the next login or the next `bootstrap`. Start it again with the
`launchctl bootstrap` line above.

**Remove it.**

```bash
launchctl bootout gui/$(id -u)/io.github.vilosource.herddash
rm ~/Library/LaunchAgents/io.github.vilosource.herddash.plist
```

Remove `~/.local/bin/herddash`, `~/.config/herddash` and the data directory too if you no longer
want the binary, the config or the record.

**How it restarts.** A clean stop (logout, `bootout`, SIGTERM) exits 0 and is not restarted. A
crash, any non-zero exit, is restarted, but never sooner than 30 seconds after the last start
(`ThrottleInterval` 30).

**launchd has no start limit.** A failure that repeats, such as a configuration error that exits 1
on every start, is retried every 30 seconds for as long as you are logged in, and each attempt adds
to `~/.local/share/herddash/herddash.stderr.log`. If `launchctl print` shows a non-zero `last exit`
and no `pid`, stop the retries, read the reason, fix it, and start again:

```bash
launchctl bootout gui/$(id -u)/io.github.vilosource.herddash
while pgrep -x -u "$(id -u)" herddash >/dev/null; do sleep 1; done
tail -n 20 ~/.local/share/herddash/herddash.stderr.log
```

Fix the cause, then empty the log and load the agent again:

```bash
: > ~/.local/share/herddash/herddash.stderr.log
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/io.github.vilosource.herddash.plist
```

## 3b. Linux: the systemd user unit

The unit is `deploy/systemd/herddash.service`. It uses `%h` for your home directory, so it is
copied as is. It also unsets `HERDR_SOCKET_PATH` and `HERDR_SESSION` (`UnsetEnvironment`), so a
value the user manager has imported, for example from a herdr pane, never reaches herddash.

**Install and start.**

```bash
mkdir -p ~/.config/systemd/user
cp deploy/systemd/herddash.service ~/.config/systemd/user/herddash.service
systemctl --user daemon-reload
systemctl --user enable --now herddash.service
```

`enable` makes it start with your session; `--now` also starts it immediately.

**Check it.**

```bash
systemctl --user status herddash.service
```

**Restart it**, after a config change or a new binary:

```bash
systemctl --user restart herddash.service
```

**Stop it** for now, leaving it enabled for the next login:

```bash
systemctl --user stop herddash.service
```

**Remove it.**

```bash
systemctl --user disable --now herddash.service
rm ~/.config/systemd/user/herddash.service
systemctl --user daemon-reload
```

**How it restarts.** A clean stop exits 0 and is not restarted (`Restart=on-failure`). A crash is
restarted after 30 seconds (`RestartSec=30`). Five failed starts within ten minutes stop the
retries (`StartLimitBurst=5`, `StartLimitIntervalSec=600`), so a broken config cannot loop. Read
why with `journalctl --user -u herddash.service`, fix it, then clear that state and start again:

```bash
systemctl --user reset-failed herddash.service
systemctl --user start herddash.service
```

**Running without a login (optional).** A user unit runs while you are logged in. To keep herddash
running when you are not, for example on a machine you reach only over SSH, enable lingering for
your own user:

```bash
loginctl enable-linger
```

`loginctl disable-linger` undoes it. herddash only records while herdr itself is running; without
it, herddash keeps retrying the socket.

## Upgrading the unit files

When `deploy/` changes in a new version, install the new unit over the old one.

macOS:

```bash
sed "s|__HOME__|$HOME|g" deploy/launchd/io.github.vilosource.herddash.plist \
  > ~/Library/LaunchAgents/io.github.vilosource.herddash.plist
grep -F "<string>$HOME/.local/bin/herddash</string>" \
  ~/Library/LaunchAgents/io.github.vilosource.herddash.plist && echo "plist OK"
launchctl bootout gui/$(id -u)/io.github.vilosource.herddash 2>/dev/null
while pgrep -x -u "$(id -u)" herddash >/dev/null; do sleep 1; done
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/io.github.vilosource.herddash.plist
```

Linux:

```bash
cp deploy/systemd/herddash.service ~/.config/systemd/user/herddash.service
systemctl --user daemon-reload
systemctl --user restart herddash.service
```

A new binary alone needs only a restart (`launchctl kickstart -k …` or
`systemctl --user restart herddash.service`).

## 4. Open the Board

The Board is at **<http://127.0.0.1:7318/>**, or at the `bind` address from your config. It
listens on loopback only.

## Logs

| What | macOS | Linux |
|---|---|---|
| The Daemon's log (JSON) | `herddash.log` in the data directory | `herddash.log` in the data directory |
| Standard error (start-up failures, panics) | `~/.local/share/herddash/herddash.stderr.log` (fixed in the plist) | `journalctl --user -u herddash.service` |
| Standard output | discarded; it repeats the log | discarded; it repeats the log |

The data directory is `data_dir` from `herddash config`; by default `~/.local/share/herddash`.

## Backup

The whole record is one SQLite database, `database` from `herddash config` (by default
`herddash.db` in the data directory). Copy it only while the Daemon is stopped, so the copy is
consistent. Take the path from the config, not from memory:

```bash
DB=$(~/.local/bin/herddash config | sed -n 's/^  "database": "\(.*\)",$/\1/p'); echo "$DB"
BACKUP=~/herddash-backup-$(date +%Y%m%d-%H%M%S).db
```

It must print the database's path. If it prints an empty line, `herddash config` failed or its
output changed: stop here, run `~/.local/bin/herddash config` to see why, and set `DB` by hand.
Set it by hand too if the path holds `&`, `<`, `>`, `"` or `\`, which the JSON escapes. The copy
steps below print an error and copy nothing when `DB` is empty.

**Stop the Daemon and wait until it has no process.**

macOS:

```bash
launchctl bootout gui/$(id -u)/io.github.vilosource.herddash
while pgrep -x -u "$(id -u)" herddash >/dev/null; do sleep 1; done
```

`launchctl print` cannot be used to wait: once `bootout` has removed the job it prints nothing,
while the process may still be shutting down. `pgrep` waits on the process itself.

Linux:

```bash
systemctl --user stop herddash.service
while [ "$(systemctl --user show -p MainPID --value herddash.service)" != 0 ]; do sleep 1; done
```

**Copy the database**, with its `-wal` and `-shm` files if they are still there. A clean stop
normally folds them into the database and removes them, but copying them keeps the backup whole if
it did not:

```bash
if [ -n "$DB" ] && [ -n "$BACKUP" ]; then
  for s in "" -wal -shm; do [ -e "$DB$s" ] && cp -p "$DB$s" "$BACKUP$s"; done
  ls -l "$BACKUP"*
else
  echo "no backup taken: DB or BACKUP is empty" >&2
fi
```

**Start it again** with `launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/io.github.vilosource.herddash.plist`
on macOS or `systemctl --user start herddash.service` on Linux.

**To restore**, stop the Daemon and wait as above, then remove the current `-wal` and `-shm`
files before copying the backup in. A leftover write-ahead log from the newer database would
otherwise be applied on top of the older backup:

```bash
if [ -n "$DB" ] && [ -e "$BACKUP" ]; then
  rm -f "$DB-wal" "$DB-shm"
  for s in "" -wal -shm; do [ -e "$BACKUP$s" ] && cp -p "$BACKUP$s" "$DB$s"; done
  ls -l "$DB"*
else
  echo "not restored: DB is empty or BACKUP is not a file" >&2
fi
```

Here `$BACKUP` is the backup you are restoring, for example
`BACKUP=~/herddash-backup-20261021-093000.db`. Then start the Daemon again.
