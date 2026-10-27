package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/vilosource/herddash/internal/config"
	"github.com/vilosource/herddash/internal/engine"
	"github.com/vilosource/herddash/internal/httpapi"
	"github.com/vilosource/herddash/internal/store"
)

// logFileName is the log file in the data directory.
const logFileName = "herddash.log"

// listen is net.Listen; a test replaces it to make the server fail while
// serving.
var listen = net.Listen

// readHeaderTimeout bounds how long a client may take to send its request
// headers; shutdownTimeout bounds the graceful HTTP shutdown when serve ends.
// Both are listed under "Fixed timings" in docs/config.example.toml.
const (
	readHeaderTimeout = 10 * time.Second
	shutdownTimeout   = 5 * time.Second
)

// testSeenDwellEnv names the test seam that lowers the dwell after which
// leaving a pane marks its finish Seen (ADR-0015, Story 6.8). Only the
// Board's browser check sets it, so that it does not wait 3 s per step; it
// is not configuration, and docs/config.example.toml does not list it.
const testSeenDwellEnv = "HERDDASH_TEST_SEEN_DWELL_MS"

// testSeenDwell is HERDDASH_TEST_SEEN_DWELL_MS as a duration: 0, the
// engine's default, when it is unset; a whole number of milliseconds from 1
// to 60000 is used and logged at warn, so a Daemon running with it says so;
// anything else is ignored with a warning.
func testSeenDwell(logger *slog.Logger) time.Duration {
	v, ok := os.LookupEnv(testSeenDwellEnv)
	if !ok {
		return 0
	}
	ms, err := strconv.Atoi(v)
	if err != nil || ms < 1 || ms > 60000 {
		logger.Warn("test seam ignored: not a whole number of milliseconds from 1 to 60000", "env", testSeenDwellEnv, "value", v)
		return 0
	}
	logger.Warn("test seam: the Seen dwell is lowered", "env", testSeenDwellEnv, "ms", ms)
	return time.Duration(ms) * time.Millisecond
}

// runServe is "herddash serve [flags]", also run by "herddash" alone. It loads
// configuration as "herddash config" does, ensures the data directory, logs
// JSON to herddash.log there, opens the database (so a second serve on the
// same database fails at once) for the engine to record every Transition into,
// listens on the configured loopback bind for the HTTP surface, read-only
// apart from acknowledgement's write routes (ADR-0007, ADR-0014), prints the address it serves on, and prints every version gate
// outcome, Agent, Transition, removal, Profile change, reconnect and resync to
// stdout until ctx ends (exit 0). herdr being absent, gone or restarting never
// ends it: the engine reconnects. Exit 1 is left for configuration, data
// directory, database and listen errors before the engine starts, and for the
// HTTP server failing while serving, which stops the engine. When ctx ends,
// the HTTP server shuts down gracefully first, and only then is the engine,
// which runs under its own context, stopped and waited for. Shutdown ends
// every open /events stream through the handler's CloseStreams, registered
// with RegisterOnShutdown; without it Shutdown would wait out its timeout on
// those long-lived responses. A store write the engine has begun finishes,
// since store writes take no context, and the deferred Store.Close then
// releases the database lock, so a serve started straight afterwards starts
// cleanly (Story 4.1, TestServeShutdownAndRestart).
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

	// Listen before the engine starts, so a port in use fails at once. The
	// bind is loopback: config refuses any other.
	ln, err := listen("tcp", cfg.Bind())
	if err != nil {
		logger.Error("listen", "bind", cfg.Bind(), "error", err.Error())
		fmt.Fprintf(stderr, "herddash: %v\n", err)
		return 1
	}
	logger.Info("listening", "addr", ln.Addr().String())

	// The home directory only shortens project directories in Agent views;
	// without one ("" on error) they are shown in full.
	home, err := os.UserHomeDir()
	if err != nil {
		logger.Warn("home directory unknown; project directories will be shown unshortened", "error", err.Error())
	}
	e := engine.New(engine.Options{
		Host:   cfg.Host(),
		Server: cfg.Server(),
		Client: engine.NewHerdrClient(cfg.SocketPath()),
		Sink:   printer{w: stdout},
		Clock:  time.Now,
		Logger: logger,

		AllowUnsupportedHerdr: cfg.AllowUnsupportedHerdr(),
		ClaudeRecaps:          cfg.ClaudeRecaps(),
		Profiles:              cfg.ProfileRules(),
		Store:                 st,
		RetentionDays:         cfg.RetentionDays(),
		Home:                  home,
		SeenDwell:             testSeenDwell(logger),
	})
	h := httpapi.New(httpapi.Options{View: e.View, Subscribe: e.Subscribe, History: st.History,
		Acknowledge: e.Acknowledge, Acknowledgements: st.Acknowledgements, Logger: logger})
	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: readHeaderTimeout,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}
	srv.RegisterOnShutdown(h.CloseStreams)
	// The engine runs under its own context so that, when ctx ends, the HTTP
	// server is shut down before the engine stops.
	engineCtx, stopEngine := context.WithCancel(context.WithoutCancel(ctx))
	defer stopEngine()

	fmt.Fprintf(stdout, "%s listening  http://%s\n", stamp(time.Now().UnixMilli()), ln.Addr())
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	engineDone := make(chan struct{})
	go func() {
		defer close(engineDone)
		e.Run(engineCtx)
	}()

	code := 0
	serveEnded := false
	select {
	case <-ctx.Done():
	case err := <-served:
		// Serve returns only on Shutdown or Close, neither called yet: this is
		// a failure, and serve without its API must not go on.
		serveEnded = true
		logger.Error("http server", "error", err.Error())
		fmt.Fprintf(stderr, "herddash: %v\n", err)
		code = 1
	}
	sctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	if err := srv.Shutdown(sctx); err != nil {
		logger.Warn("http shutdown", "error", err.Error())
		srv.Close()
	}
	cancel()
	if !serveEnded {
		if err := <-served; err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server", "error", err.Error())
		}
	}
	stopEngine()
	<-engineDone
	logger.Info("herddash stopped")
	return code
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
