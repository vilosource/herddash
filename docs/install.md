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

Build it (Go 1.26 or newer):

```bash
mkdir -p ~/.local/bin
go build -o ~/.local/bin/herddash ./cmd/herddash
```

**Once release binaries are published (Story 4.3)**, you can instead download the one for your
platform from the GitHub release, check it against the release's SHA-256 checksums, and move it to
`~/.local/bin/herddash` with `chmod 755`. On macOS a file downloaded through a browser is
quarantined; clear that with `xattr -d com.apple.quarantine ~/.local/bin/herddash`. Until then,
build it.

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
already loaded, so unload any earlier copy first:

```bash
launchctl bootout gui/$(id -u)/io.github.vilosource.herddash 2>/dev/null
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

**Stop it.**

```bash
launchctl bootout gui/$(id -u)/io.github.vilosource.herddash
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
tail -n 20 ~/.local/share/herddash/herddash.stderr.log
# fix the cause, then empty the log and load the agent again:
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

**Stop the Daemon and wait until it has no process.**

macOS:

```bash
launchctl bootout gui/$(id -u)/io.github.vilosource.herddash
while launchctl print gui/$(id -u)/io.github.vilosource.herddash 2>/dev/null | grep -q 'pid ='; do sleep 1; done
```

Linux:

```bash
systemctl --user stop herddash.service
while [ "$(systemctl --user show -p MainPID --value herddash.service)" != 0 ]; do sleep 1; done
```

**Copy the database**, with its `-wal` and `-shm` files if they are still there. A clean stop
normally folds them into the database and removes them, but copying them keeps the backup whole if
it did not:

```bash
for s in "" -wal -shm; do [ -e "$DB$s" ] && cp -p "$DB$s" "$BACKUP$s"; done
ls -l "$BACKUP"*
```

**Start it again** with `launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/io.github.vilosource.herddash.plist`
on macOS or `systemctl --user start herddash.service` on Linux.

**To restore**, stop the Daemon and wait as above, then remove the current `-wal` and `-shm`
files before copying the backup in. A leftover write-ahead log from the newer database would
otherwise be applied on top of the older backup:

```bash
rm -f "$DB-wal" "$DB-shm"
for s in "" -wal -shm; do [ -e "$BACKUP$s" ] && cp -p "$BACKUP$s" "$DB$s"; done
```

Here `$BACKUP` is the backup you are restoring, for example
`BACKUP=~/herddash-backup-20261021-093000.db`. Then start the Daemon again.
