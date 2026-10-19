package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/vilosource/herddash/internal/config"
	"github.com/vilosource/herddash/internal/importer"
	"github.com/vilosource/herddash/internal/store"
)

// sinceFlag is the import's required flag: the date the Host reached herdr
// 0.9.2, before which a done entry may be spurious.
const sinceFlag = "herdr-0.9.2-since"

// runImport is "herddash import-plugin-log <file> --herdr-0.9.2-since
// <YYYY-MM-DD> [flags]". It runs with no Daemon: it opens the database as
// serve does, so a running serve makes it fail at once naming the lock, then
// writes the log's entries as backfilled history in one transaction and prints
// one summary line. Exit 2 is a usage error, exit 1 any other failure.
func runImport(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("herddash import-plugin-log", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintf(stderr, `usage: herddash import-plugin-log <file> --%s <YYYY-MM-DD> [flags]

Imports the agent-webhook plugin's log as history. No serve may be running.
  - It takes serve's configuration flags: the rows' Host and Server come
    from them, so give the same ones serve uses.
  - The log's times are read in this machine's local time zone.
  - done entries before the date are flagged possibly spurious; a Host that
    never ran herdr 0.9.2 or newer passes a date after the last entry, such
    as tomorrow.

`, sinceFlag)
		fs.PrintDefaults()
	}
	given := configFlags(fs)
	var sinceText string
	fs.StringVar(&sinceText, sinceFlag, "", "the date this Host reached herdr 0.9.2 (required); done entries before it are flagged possibly spurious")
	// The file may come before the flags, so parse around positionals.
	var files []string
	for rest := args; ; {
		if err := fs.Parse(rest); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return 0
			}
			return 2
		}
		if fs.NArg() == 0 {
			break
		}
		// After "--", every remaining argument is a file.
		if i := len(rest) - fs.NArg() - 1; i >= 0 && rest[i] == "--" {
			files = append(files, fs.Args()...)
			break
		}
		files = append(files, fs.Arg(0))
		rest = fs.Args()[1:]
	}
	if len(files) != 1 {
		fmt.Fprintf(stderr, "herddash: import-plugin-log takes one log file, got %q\n", files)
		return 2
	}
	sinceSet := false
	fs.Visit(func(fl *flag.Flag) { sinceSet = sinceSet || fl.Name == sinceFlag })
	if !sinceSet {
		fmt.Fprintf(stderr, "herddash: --%s is required\n", sinceFlag)
		return 2
	}
	since, err := importer.ParseDate(sinceText, time.Local)
	if err != nil {
		fmt.Fprintf(stderr, "herddash: --%s: %v\n", sinceFlag, err)
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
	f, err := os.Open(files[0])
	if err != nil {
		fmt.Fprintf(stderr, "herddash: %v\n", err)
		return 1
	}
	defer f.Close()
	// Refuse a directory or device before creating anything.
	if fi, err := f.Stat(); err != nil {
		fmt.Fprintf(stderr, "herddash: %v\n", err)
		return 1
	} else if !fi.Mode().IsRegular() {
		fmt.Fprintf(stderr, "herddash: %s is not a regular file\n", files[0])
		return 1
	}
	if err := config.EnsureDataDir(cfg.DataDir()); err != nil {
		fmt.Fprintf(stderr, "herddash: %v\n", err)
		return 1
	}
	// A running Daemon holds the lock: fail at once, writing nothing.
	st, err := store.Open(cfg.Database())
	if err != nil {
		fmt.Fprintf(stderr, "herddash: %v\n", err)
		return 1
	}
	// The transaction is not cancelled by a signal: a rollback left running
	// after Close would hold the database busy. It is one local write.
	sum, err := importer.Import(context.WithoutCancel(ctx), st, f, cfg, since, time.Now())
	if cerr := st.Close(); err == nil && cerr != nil {
		err = cerr
	}
	if err != nil {
		fmt.Fprintf(stderr, "herddash: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, sum)
	return 0
}
