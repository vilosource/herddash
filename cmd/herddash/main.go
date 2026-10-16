// Command herddash records every herdr agent status transition and serves a
// read-only view of them. Only the version subcommand exists so far.
package main

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"
)

// Stamped at release time with -ldflags "-X main.version=... -X main.commit=...".
var (
	version = "dev"
	commit  = ""
)

const usage = `usage: herddash <command>

commands:
  version   print the version and commit
`

func main() {
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
	case "help", "-h", "-help", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "herddash: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
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
