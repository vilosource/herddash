package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/vilosource/herddash/internal/config"
	"github.com/vilosource/herddash/internal/engine"
	"github.com/vilosource/herddash/internal/store"
)

// logFileName is the log file in the data directory.
const logFileName = "herddash.log"

// runServe is "herddash serve [flags]", also run by "herddash" alone. It loads
// configuration as "herddash config" does, ensures the data directory, logs
// JSON to herddash.log there, opens the database (so a second serve on the
// same database fails at once) for the engine to record every Transition into,
// and prints every version gate outcome, Agent, Transition, removal, Profile
// change, reconnect and resync to stdout until ctx ends (exit 0). herdr being
// absent, gone or restarting never ends it: the engine reconnects. Exit 1 is
// left for configuration, data directory and database errors before the
// engine starts.
func runServe(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("herddash serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	given := configFlags(fs)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "herddash: serve takes no arguments, got %q\n", fs.Args())
		return 2
	}
	env, err := config.OSEnv()
	if err != nil {
		fmt.Fprintf(stderr, "herddash: %v\n", err)
		return 1
	}
	cfg, err := config.Load(given(), env)
	if err != nil {
		fmt.Fprintf(stderr, "herddash: %v\n", err)
		return 1
	}
	if err := config.EnsureDataDir(cfg.DataDir()); err != nil {
		fmt.Fprintf(stderr, "herddash: %v\n", err)
		return 1
	}
	// Open the store before the log: a refused second serve must not write
	// into the running Daemon's log. Holding it open is what makes this
	// process the database's only owner; the engine records into it.
	st, err := store.Open(cfg.Database())
	if err != nil {
		fmt.Fprintf(stderr, "herddash: %v\n", err)
		return 1
	}
	logPath := filepath.Join(cfg.DataDir(), logFileName)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		st.Close()
		fmt.Fprintf(stderr, "herddash: open log: %v\n", err)
		return 1
	}
	defer logFile.Close()
	// An existing log keeps its mode on open; make it owner-only (FR-17).
	if err := logFile.Chmod(0o600); err != nil {
		st.Close()
		fmt.Fprintf(stderr, "herddash: log: %v\n", err)
		return 1
	}
	logger := slog.New(slog.NewJSONHandler(logFile, nil))
	logger.Info("herddash starting", "version", version, "commit", resolveCommit(),
		"host", cfg.Host(), "server", cfg.Server(), "socket", cfg.SocketPath())
	defer func() {
		if err := st.Close(); err != nil {
			logger.Error("database close", "err", err)
		}
	}()

	e := engine.New(engine.Options{
		Host:   cfg.Host(),
		Server: cfg.Server(),
		Client: engine.NewHerdrClient(cfg.SocketPath()),
		Sink:   printer{w: stdout},
		Clock:  time.Now,
		Logger: logger,

		AllowUnsupportedHerdr: cfg.AllowUnsupportedHerdr(),
		Profiles:              cfg.ProfileRules(),
		Store:                 st,
		RetentionDays:         cfg.RetentionDays(),
	})
	e.Run(ctx)
	logger.Info("herddash stopped")
	return 0
}

// printer is the engine Sink that prints one line per gate outcome, per Agent
// first seen, per Transition, per removal, per Profile change, per reconnect
// and per resync. Agent and Transition lines carry the Agent's Attention Class
// and Waiting Time at the line's time. The engine calls it from one goroutine.
type printer struct{ w io.Writer }

func (p printer) Agent(a engine.Agent, observedAt int64) {
	fmt.Fprintf(p.w, "%s agent      host=%s epoch=%d pane=%s program=%s profile=%s status=%s %s\n",
		stamp(observedAt), a.Key.Host, a.Key.Epoch, a.Key.PaneID, orDash(string(a.AgentProgram)), orDash(a.Profile), a.Status,
		waiting(a, observedAt))
}

func (p printer) Transition(t engine.Transition, a engine.Agent) {
	prev, ok := t.Previous()
	if !ok {
		prev = "-"
	}
	fmt.Fprintf(p.w, "%s transition host=%s epoch=%d pane=%s program=%s profile=%s status=%s previous=%s provenance=%s herdr=%s %s\n",
		stamp(t.ObservedAt()), t.Key().Host, t.Key().Epoch, t.Key().PaneID, orDash(string(t.AgentProgram())), orDash(t.Profile()),
		t.New(), prev, t.Provenance(), orDash(t.HerdrVersion()), waiting(a, t.ObservedAt()))
}

func (p printer) Removed(a engine.Agent, observedAt int64) {
	fmt.Fprintf(p.w, "%s removed    host=%s epoch=%d pane=%s program=%s profile=%s status=%s\n",
		stamp(observedAt), a.Key.Host, a.Key.Epoch, a.Key.PaneID, orDash(string(a.AgentProgram)), orDash(a.Profile), a.Status)
}

func (p printer) Profile(a engine.Agent, previous string, observedAt int64) {
	fmt.Fprintf(p.w, "%s profile    host=%s epoch=%d pane=%s program=%s profile=%s previous=%s\n",
		stamp(observedAt), a.Key.Host, a.Key.Epoch, a.Key.PaneID, orDash(string(a.AgentProgram)), orDash(a.Profile), orDash(previous))
}

func (p printer) Reconnected(herdrVersion string, epoch uint64, kept bool, observedAt int64) {
	change := "incremented"
	if kept {
		change = "kept"
	}
	fmt.Fprintf(p.w, "%s reconnect  herdr=%s epoch=%d epoch_change=%s\n", stamp(observedAt), herdrVersion, epoch, change)
}

func (p printer) Resynced(cause string, observedAt int64) {
	fmt.Fprintf(p.w, "%s resync     cause=%s\n", stamp(observedAt), cause)
}

func (p printer) Gate(gate engine.Gate, herdrVersion string, observedAt int64) {
	fmt.Fprintf(p.w, "%s gate       herdr=%s minimum=%s gate=%s\n", stamp(observedAt), orDash(herdrVersion), engine.HerdrMinimum, gate)
}

// waiting formats an Agent's Attention Class and its Waiting Time at the
// line's own time at (UTC Unix ms): "class=<class> waiting=<d>", with "≥"
// before <d> when it is a lower bound and "-" when it is not known.
func waiting(a engine.Agent, at int64) string {
	class := a.Class
	if class == "" {
		class = engine.ClassUnknown
	}
	if a.WaitingSince == 0 {
		return fmt.Sprintf("class=%s waiting=-", class)
	}
	bound := "≥"
	if a.WaitingExact {
		bound = ""
	}
	return fmt.Sprintf("class=%s waiting=%s%s", class, bound, compact(max(at-a.WaitingSince, 0)))
}

// compact formats ms as a short duration, truncated to its two largest units:
// "0s", "45s", "12m", "1h05m", "2d03h".
func compact(ms int64) string {
	s := ms / 1000
	switch {
	case s < 60:
		return fmt.Sprintf("%ds", s)
	case s < 3600:
		return fmt.Sprintf("%dm", s/60)
	case s < 86400:
		return fmt.Sprintf("%dh%02dm", s/3600, s%3600/60)
	}
	return fmt.Sprintf("%dd%02dh", s/86400, s%86400/3600)
}

// stamp formats UTC Unix milliseconds for a line.
func stamp(ms int64) string {
	return time.UnixMilli(ms).UTC().Format("2006-01-02T15:04:05.000Z")
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
