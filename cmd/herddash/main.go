// Command herddash records every herdr agent status transition and serves a
// read-only view of them. So far it has the version and config subcommands.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"syscall"

	"github.com/vilosource/herddash/internal/config"
)

// Stamped at release time with -ldflags "-X main.version=... -X main.commit=...".
var (
	version = "dev"
	commit  = ""
)

const usage = `usage: herddash <command>

commands:
  config    print the effective configuration as JSON
  version   print the version and commit

Run "herddash config -h" for the configuration flags.
`

func main() {
	// Owner-only files from the start (FR-17, AD-9): the data directory is 0700
	// and the database, its WAL and SHM files, the lock and the log are 0600.
	// Set here rather than in run, so tests do not change the test process.
	syscall.Umask(0o077)
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "version":
		if len(args) > 1 {
			fmt.Fprintf(stderr, "herddash: version takes no arguments\n\n%s", usage)
			return 2
		}
		fmt.Fprintf(stdout, "herddash %s (%s)\n", version, resolveCommit())
		return 0
	case "config":
		return runConfig(args[1:], stdout, stderr)
	case "help", "-h", "-help", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "herddash: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

// configFlags registers the configuration flags shared by every subcommand that
// loads configuration, and returns a function that yields the flags that were
// given after parsing. A flag not given leaves its config.Flags field nil.
func configFlags(fs *flag.FlagSet) func() config.Flags {
	var f config.Flags
	var configPath, host, session, socket, server, bind, dataDir, database string
	var retention int
	var allow bool
	fs.StringVar(&configPath, "config", "", "configuration file (default "+config.DefaultConfigFile+")")
	fs.StringVar(&host, "host", "", "this Host's name (default: short OS hostname)")
	fs.StringVar(&session, "session", "", "herdr session name")
	fs.StringVar(&socket, "socket", "", "herdr socket path")
	fs.StringVar(&server, "server", "", "herdr server name (default: derived from the socket path)")
	fs.StringVar(&bind, "bind", "", "loopback address to serve on (default "+config.DefaultBind+")")
	fs.StringVar(&dataDir, "data-dir", "", "data directory (default $XDG_DATA_HOME/"+config.DataDirName+", else ~/.local/share/"+config.DataDirName+")")
	fs.StringVar(&database, "database", "", "database file (default <data-dir>/"+config.DatabaseName+")")
	fs.IntVar(&retention, "retention-days", 0, fmt.Sprintf("days of history to keep, 1 to %d (default %d)", config.MaxRetentionDays, config.DefaultRetentionDays))
	fs.BoolVar(&allow, "allow-unsupported-herdr", false, "record even when herdr is older than the supported minimum")
	return func() config.Flags {
		fs.Visit(func(fl *flag.Flag) {
			switch fl.Name {
			case "config":
				f.Config = &configPath
			case "host":
				f.Host = &host
			case "session":
				f.Session = &session
			case "socket":
				f.Socket = &socket
			case "server":
				f.Server = &server
			case "bind":
				f.Bind = &bind
			case "data-dir":
				f.DataDir = &dataDir
			case "database":
				f.Database = &database
			case "retention-days":
				f.RetentionDays = &retention
			case "allow-unsupported-herdr":
				f.AllowUnsupportedHerdr = &allow
			}
		})
		return f
	}
}

// runConfig is "herddash config [flags]": it prints the effective
// configuration as indented JSON, or the error on stderr with exit 1. It reads
// the configuration file and nothing else; it creates no directory.
func runConfig(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("herddash config", flag.ContinueOnError)
	fs.SetOutput(stderr)
	given := configFlags(fs)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "herddash: config takes no arguments, got %q\n", fs.Args())
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
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "herddash: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "%s\n", out)
	return 0
}

// resolveCommit returns the stamped commit, else the VCS revision Go embedded in
// the build, else "unknown".
func resolveCommit() string {
	var settings []debug.BuildSetting
	info, ok := debug.ReadBuildInfo()
	if ok {
		settings = info.Settings
	}
	return commitFrom(commit, settings, ok)
}

// commitFrom is resolveCommit's logic over explicit inputs: the stamped commit
// wins; otherwise the build's vcs.revision, shortened to 12 characters and
// suffixed "-dirty" when vcs.modified is true; otherwise "unknown".
func commitFrom(stamped string, settings []debug.BuildSetting, ok bool) string {
	if stamped != "" {
		return stamped
	}
	if !ok {
		return "unknown"
	}
	var rev string
	var dirty bool
	for _, s := range settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return "unknown"
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if dirty {
		rev += "-dirty"
	}
	return rev
}
