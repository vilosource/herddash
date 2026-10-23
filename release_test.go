package herddash_test

// These tests check Story 4.3's release machinery: scripts/release-build.sh
// builds the four release binaries with the version stamped in and a
// SHA256SUMS file over them, and .github/workflows/release.yml builds, checks
// and attaches them to an existing release without ever creating a tag or a
// release. The workflow files are checked as text; nothing here talks to
// GitHub.

import (
	"bufio"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const (
	releaseScript   = "scripts/release-build.sh"
	releaseFind     = "scripts/release-find.sh"
	releaseWorkflow = ".github/workflows/release.yml"
	rpWorkflow      = ".github/workflows/release-please.yml"
	ciWorkflow      = ".github/workflows/ci.yml"
	spinePath       = "docs/planning/architecture/architecture-herddash-2026-10-14/ARCHITECTURE-SPINE.md"
)

var releaseTargets = []string{"darwin/arm64", "darwin/amd64", "linux/amd64", "linux/arm64"}

// scriptEnv is the environment for running the build script: the test's own,
// with this toolchain's go first on PATH so the script finds it.
func scriptEnv(extra ...string) []string {
	env := os.Environ()
	if goroot := runtime.GOROOT(); goroot != "" {
		env = append(env, "PATH="+filepath.Join(goroot, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	return append(env, extra...)
}

// releaseTreeState returns the repository's HEAD commit and whether its tree
// has modified or untracked files, as Go's vcs.revision and vcs.modified
// record them, and whether to check them at all; judgeTree decides.
func releaseTreeState(t *testing.T) (head string, dirty, check bool) {
	t.Helper()
	var status string
	_, err := exec.LookPath("git")
	if err == nil {
		git := func(args ...string) (string, error) {
			b, err := exec.Command("git", args...).Output()
			if err != nil {
				err = fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
			}
			return strings.TrimRight(string(b), "\n"), err
		}
		if head, err = git("rev-parse", "HEAD"); err == nil {
			status, err = git("status", "--porcelain")
		}
	}
	ci, _ := strconv.ParseBool(os.Getenv("CI"))
	// A linked worktree's .git is a file, not a directory.
	fi, statErr := os.Lstat(".git")
	linked := statErr == nil && fi.Mode().IsRegular()
	dirty, check, fatal, msg := judgeTree(ci, linked, status, err)
	switch {
	case fatal:
		t.Fatal(msg)
	case !check:
		t.Log(msg)
	}
	return head, dirty, check
}

// judgeTree decides what TestReleaseBuild expects of the binaries' vcs
// settings from `git status --porcelain` output, or the error that stopped git
// (no git, no repository). Without git the checks are skipped with a log
// message, unless ci is set (as GitHub sets CI); under ci a modified tree fails
// too, since CI must prove clean-tree builds. In a linked git worktree (linked:
// .git is a file) the checks are skipped outside CI: Go 1.26 does not take a
// .git file as a repository root, so a worktree nested inside another checkout
// (as .claude/worktrees/ are) gets the enclosing checkout's commit and state
// stamped in. CI checks out a plain clone, where the checks always run.
func judgeTree(ci, linked bool, status string, gitErr error) (dirty, check, fatal bool, msg string) {
	if gitErr != nil {
		if ci {
			return false, false, true, fmt.Sprintf("CI is set but the build's commit cannot be checked: %v", gitErr)
		}
		return false, false, false, fmt.Sprintf("skipping the vcs.revision and vcs.modified checks: %v", gitErr)
	}
	if linked && !ci {
		return false, false, false, "skipping the vcs.revision and vcs.modified checks: a linked git worktree, whose .git file Go 1.26 does not recognise"
	}
	dirty = status != ""
	if dirty && ci {
		return true, true, true, "CI is set but the tree is modified, so the binaries would not be clean-tree builds:\n" + status
	}
	return dirty, true, false, ""
}

func TestJudgeTree(t *testing.T) {
	noGit := errors.New(`exec: "git": executable file not found in $PATH`)
	notRepo := fmt.Errorf("git rev-parse HEAD: %w", errors.New("exit status 128"))
	for _, c := range []struct {
		name                string
		ci, linked          bool
		status              string
		err                 error
		dirty, check, fatal bool
	}{
		{"clean", false, false, "", nil, false, true, false},
		{"clean under CI", true, false, "", nil, false, true, false},
		{"dirty locally", false, false, " M release_test.go", nil, true, true, false},
		{"untracked locally", false, false, "?? new.go", nil, true, true, false},
		{"dirty under CI", true, false, "?? new.go", nil, true, true, true},
		{"no git locally", false, false, "", noGit, false, false, false},
		{"no git under CI", true, false, "", noGit, false, false, true},
		{"tracked modification under CI", true, false, " M release_test.go", nil, true, true, true},
		{"not a repository locally", false, false, "", notRepo, false, false, false},
		{"not a repository under CI", true, false, "", notRepo, false, false, true},
		{"linked worktree locally", false, true, " M release_test.go", nil, false, false, false},
		{"linked worktree under CI", true, true, "", nil, false, true, false},
		{"dirty linked worktree under CI", true, true, "?? new.go", nil, true, true, true},
	} {
		dirty, check, fatal, msg := judgeTree(c.ci, c.linked, c.status, c.err)
		if dirty != c.dirty || check != c.check || fatal != c.fatal {
			t.Errorf("%s: dirty %v check %v fatal %v, want %v %v %v", c.name, dirty, check, fatal, c.dirty, c.check, c.fatal)
		}
		if (fatal || !check) && msg == "" {
			t.Errorf("%s: no message", c.name)
		}
	}
}

func TestReleaseBuild(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not found")
	}
	host := runtime.GOOS + "/" + runtime.GOARCH
	targets := releaseTargets
	var extra []string
	if testing.Short() {
		if !slices.Contains(releaseTargets, host) {
			t.Skipf("host %s is not a release target", host)
		}
		targets = []string{host}
		extra = append(extra, "HERDDASH_TARGETS="+host)
	}

	// The commit and tree state Go should stamp into every binary, read
	// before building.
	head, dirty, checkVCS := releaseTreeState(t)

	// A nested directory that does not exist yet: the script creates it.
	out := filepath.Join(t.TempDir(), "dist", "nested")
	cmd := exec.Command("bash", releaseScript, "v0.1.0", "abc1234", out)
	cmd.Env = scriptEnv(extra...)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s: %v\n%s", releaseScript, err, b)
	}

	var want []string
	for _, tg := range targets {
		goos, goarch, _ := strings.Cut(tg, "/")
		want = append(want, "herddash_0.1.0_"+goos+"_"+goarch)
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	wantFiles := append(slices.Clone(want), "SHA256SUMS")
	slices.Sort(got)
	slices.Sort(wantFiles)
	if !slices.Equal(got, wantFiles) {
		t.Fatalf("%s holds %q, want exactly %q", out, got, wantFiles)
	}

	// SHA256SUMS: one "<hex>  <name>" line per binary, each hash correct.
	sums, err := os.Open(filepath.Join(out, "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	defer sums.Close()
	listed := map[string]bool{}
	line := regexp.MustCompile(`^([0-9a-f]{64})  (\S+)$`)
	sc := bufio.NewScanner(sums)
	for sc.Scan() {
		m := line.FindStringSubmatch(sc.Text())
		if m == nil {
			t.Errorf("SHA256SUMS line %q is not \"<sha256>  <name>\"", sc.Text())
			continue
		}
		data, err := os.ReadFile(filepath.Join(out, m[2]))
		if err != nil {
			t.Errorf("SHA256SUMS names %s: %v", m[2], err)
			continue
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != m[1] {
			t.Errorf("SHA256SUMS hash for %s is wrong", m[2])
		}
		listed[m[2]] = true
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	for _, n := range want {
		if !listed[n] {
			t.Errorf("SHA256SUMS does not cover %s", n)
		}
	}
	if len(listed) != len(want) {
		t.Errorf("SHA256SUMS covers %d files, want %d", len(listed), len(want))
	}

	// Every binary's build info: its target, no cgo, -trimpath. Go leaves
	// -ldflags out of the build info under -trimpath, so the stamped version
	// and commit are checked by running the host binary below; all the
	// targets are built from one ldflags value.
	for i, tg := range targets {
		goos, goarch, _ := strings.Cut(tg, "/")
		info, err := buildinfo.ReadFile(filepath.Join(out, want[i]))
		if err != nil {
			t.Errorf("%s: %v", want[i], err)
			continue
		}
		settings := map[string]string{}
		for _, st := range info.Settings {
			settings[st.Key] = st.Value
		}
		for k, v := range map[string]string{
			"GOOS": goos, "GOARCH": goarch, "CGO_ENABLED": "0", "-trimpath": "true",
		} {
			if settings[k] != v {
				t.Errorf("%s: build setting %s = %q, want %q", want[i], k, settings[k], v)
			}
		}
		if info.Path != "github.com/vilosource/herddash/cmd/herddash" {
			t.Errorf("%s: built from %s, not cmd/herddash", want[i], info.Path)
		}
		// The commit it was built from, and whether the tree was modified:
		// Go keeps both under -trimpath, and release.yml checks them.
		if checkVCS {
			if got := settings["vcs.revision"]; got != head {
				t.Errorf("%s: vcs.revision = %q, want HEAD %s", want[i], got, head)
			}
			if got, wantMod := settings["vcs.modified"], strconv.FormatBool(dirty); got != wantMod {
				t.Errorf("%s: vcs.modified = %q, want %s (the tree's state)", want[i], got, wantMod)
			}
		}
	}

	if !slices.Contains(releaseTargets, host) {
		return
	}
	bin := filepath.Join(out, "herddash_0.1.0_"+runtime.GOOS+"_"+runtime.GOARCH)
	b, err := exec.Command(bin, "version").CombinedOutput()
	if err != nil {
		t.Fatalf("%s version: %v\n%s", bin, err, b)
	}
	if got, want := strings.TrimSpace(string(b)), "herddash 0.1.0 (abc1234)"; got != want {
		t.Errorf("version printed %q, want %q", got, want)
	}
}

func TestReleaseBuildRejectsBadArguments(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not found")
	}
	// A fresh path: a rejected run must not even create it. The commits are
	// values the linker would accept, so only the script's guard stops them.
	dir := filepath.Join(t.TempDir(), "out")
	for name, c := range map[string]struct {
		args []string
		env  []string
	}{
		"too few":               {args: []string{"0.1.0", "abc1234"}},
		"empty version":         {args: []string{"", "abc1234", dir}},
		"bare v":                {args: []string{"v", "abc1234", dir}},
		"two-part version":      {args: []string{"0.1", "abc1234", dir}},
		"build metadata":        {args: []string{"0.1.0+build", "abc1234", dir}},
		"leading dash":          {args: []string{"-0.1.0", "abc1234", dir}},
		"empty prerelease":      {args: []string{"0.1.0-", "abc1234", dir}},
		"prerelease underscore": {args: []string{"0.1.0-rc_1", "abc1234", dir}},
		"empty commit":          {args: []string{"0.1.0", "", dir}},
		"commit with slash":     {args: []string{"0.1.0", "abc/1234", dir}},
		"commit smuggling -X":   {args: []string{"0.1.0", "abc -X main.version=evil", dir}},
		"commit leading dash":   {args: []string{"0.1.0", "-abc", dir}},
		"empty outdir":          {args: []string{"0.1.0", "abc1234", ""}},
		"unknown target":        {args: []string{"0.1.0", "abc1234", dir}, env: []string{"HERDDASH_TARGETS=windows/amd64"}},
		"blank targets":         {args: []string{"0.1.0", "abc1234", dir}, env: []string{"HERDDASH_TARGETS= "}},
		"empty targets":         {args: []string{"0.1.0", "abc1234", dir}, env: []string{"HERDDASH_TARGETS="}},
		"duplicate target":      {args: []string{"0.1.0", "abc1234", dir}, env: []string{"HERDDASH_TARGETS=linux/amd64 linux/amd64"}},
	} {
		cmd := exec.Command("bash", append([]string{releaseScript}, c.args...)...)
		cmd.Env = scriptEnv(c.env...)
		b, err := cmd.CombinedOutput()
		// Exit 2 is the script's own argument guard, not a failed build.
		if code := cmd.ProcessState.ExitCode(); err == nil || code != 2 {
			t.Errorf("%s: exit %d (%v), want the guard's exit 2\n%s", name, code, err, b)
		}
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("a rejected run created %s (stat: %v)", dir, err)
	}
}

// The tag rule in release-find.sh is the version rule in release-build.sh
// with a v, so every tag the finder accepts is a version the builder accepts.
func TestReleaseVersionRulesAgree(t *testing.T) {
	re := func(path, name string) string {
		m := regexp.MustCompile(`(?m)^` + name + `='([^']*)'$`).FindStringSubmatch(readText(t, path))
		if m == nil {
			t.Fatalf("%s has no %s='...' line", path, name)
		}
		return m[1]
	}
	version := re(releaseScript, "version_re")
	tag := re(releaseFind, "tag_re")
	if want := "^v" + strings.TrimPrefix(version, "^"); tag != want {
		t.Errorf("release-find.sh tag_re = %q, want %q", tag, want)
	}
	if !strings.Contains(readText(t, releaseWorkflow), "scripts/release-find.sh") {
		t.Errorf("%s does not use scripts/release-find.sh", releaseWorkflow)
	}
}

// releaseRepo makes a git repository with two commits and returns its
// directory, a git runner for it, and the two commits' full hashes.
func releaseRepo(t *testing.T) (string, func(...string) string, string, string) {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=test", "-c", "user.email=test@example.com",
			"-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}, args...)...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		b, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, b)
		}
		return strings.TrimSpace(string(b))
	}
	git("init", "-q")
	git("commit", "-q", "--allow-empty", "-m", "one")
	c1 := git("rev-parse", "HEAD")
	git("commit", "-q", "--allow-empty", "-m", "two")
	c2 := git("rev-parse", "HEAD")
	return dir, git, c1, c2
}

// stubGH writes a gh that answers `gh release view <tag> ...` from files in
// dir: <tag> holds the asset count, <tag>.err a failure message; neither means
// "release not found". It returns the PATH entry to put first.
func stubGH(t *testing.T) (bin, state string) {
	t.Helper()
	bin = t.TempDir()
	state = t.TempDir()
	script := `#!/bin/sh
[ "$1 $2" = "release view" ] || { echo "stub gh: unexpected: $*" >&2; exit 3; }
f="$STUB_GH_STATE/$3"
if [ -f "$f.err" ]; then cat "$f.err" >&2; exit 1; fi
if [ -f "$f" ]; then cat "$f"; exit 0; fi
echo "release not found" >&2
exit 1
`
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return bin, state
}

func TestReleaseFind(t *testing.T) {
	for _, tool := range []string{"bash", "git"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found", tool)
		}
	}
	finder, err := filepath.Abs(releaseFind)
	if err != nil {
		t.Fatal(err)
	}

	type release struct{ assets, err string }
	cases := []struct {
		name     string
		head     int            // which commit is checked out: 1 or 2
		tags     map[string]int // tag -> commit (1 or 2)
		releases map[string]release
		event    string
		ref      string // "c1"/"c2" stand for those commits' hashes
		force    string
		ok       bool
		want     map[string]string // checked when ok; commit "c2" means its short hash
	}{
		{
			name: "tagged commit with a release", head: 2,
			tags: map[string]int{"v0.1.0": 2}, releases: map[string]release{"v0.1.0": {assets: "0"}},
			event: "workflow_run", ref: "c2", force: "false", ok: true,
			want: map[string]string{"found": "true", "tag": "v0.1.0", "version": "0.1.0", "commit": "c2"},
		},
		{
			name: "prerelease tag", head: 2,
			tags: map[string]int{"v0.2.0-rc.1": 2}, releases: map[string]release{"v0.2.0-rc.1": {assets: "0"}},
			event: "workflow_run", ref: "c2", force: "false", ok: true,
			want: map[string]string{"found": "true", "tag": "v0.2.0-rc.1", "version": "0.2.0-rc.1", "commit": "c2"},
		},
		{
			name: "untagged commit", head: 1,
			tags: map[string]int{"v0.1.0": 2}, releases: map[string]release{"v0.1.0": {assets: "0"}},
			event: "workflow_run", ref: "c1", force: "false", ok: true,
			want: map[string]string{"found": "false"},
		},
		{
			name: "tag without a release", head: 2,
			tags:  map[string]int{"v0.1.0": 2},
			event: "workflow_run", ref: "c2", force: "false", ok: true,
			want: map[string]string{"found": "false"},
		},
		{
			name: "gh fails on workflow_run", head: 2,
			tags: map[string]int{"v0.1.0": 2}, releases: map[string]release{"v0.1.0": {err: "HTTP 502: Bad Gateway"}},
			event: "workflow_run", ref: "c2", force: "false",
		},
		{
			name: "two tags with releases", head: 2,
			tags:     map[string]int{"v0.1.0": 2, "v0.1.1": 2},
			releases: map[string]release{"v0.1.0": {assets: "0"}, "v0.1.1": {assets: "0"}},
			event:    "workflow_run", ref: "c2", force: "false",
		},
		{
			name: "HEAD is not the run's commit", head: 1,
			tags: map[string]int{"v0.1.0": 2}, releases: map[string]release{"v0.1.0": {assets: "0"}},
			event: "workflow_run", ref: "c2", force: "false",
		},
		{
			name: "malformed release tag", head: 2,
			tags: map[string]int{"v0.1": 2}, releases: map[string]release{"v0.1": {assets: "0"}},
			event: "workflow_run", ref: "c2", force: "false",
		},
		{
			name: "release already has assets", head: 2,
			tags: map[string]int{"v0.1.0": 2}, releases: map[string]release{"v0.1.0": {assets: "5"}},
			event: "workflow_run", ref: "c2", force: "false",
		},
		{
			name: "dispatch with a release", head: 2,
			tags: map[string]int{"v0.1.0": 2}, releases: map[string]release{"v0.1.0": {assets: "0"}},
			event: "workflow_dispatch", ref: "v0.1.0", force: "false", ok: true,
			want: map[string]string{"found": "true", "tag": "v0.1.0", "version": "0.1.0", "commit": "c2"},
		},
		{
			name: "dispatch with no release", head: 2,
			tags:  map[string]int{"v0.1.0": 2},
			event: "workflow_dispatch", ref: "v0.1.0", force: "false",
		},
		{
			name: "dispatch tag not at HEAD", head: 1,
			tags: map[string]int{"v0.1.0": 2}, releases: map[string]release{"v0.1.0": {assets: "0"}},
			event: "workflow_dispatch", ref: "v0.1.0", force: "false",
		},
		{
			name: "dispatch when gh fails", head: 2,
			tags: map[string]int{"v0.1.0": 2}, releases: map[string]release{"v0.1.0": {err: "HTTP 401: Bad credentials"}},
			event: "workflow_dispatch", ref: "v0.1.0", force: "false",
		},
		{
			name: "dispatch over existing assets", head: 2,
			tags: map[string]int{"v0.1.0": 2}, releases: map[string]release{"v0.1.0": {assets: "5"}},
			event: "workflow_dispatch", ref: "v0.1.0", force: "false",
		},
		{
			name: "dispatch over existing assets with force", head: 2,
			tags: map[string]int{"v0.1.0": 2}, releases: map[string]release{"v0.1.0": {assets: "5"}},
			event: "workflow_dispatch", ref: "v0.1.0", force: "true", ok: true,
			want: map[string]string{"found": "true", "tag": "v0.1.0", "version": "0.1.0", "commit": "c2"},
		},
		{
			name: "dispatch with a malformed tag", head: 2,
			tags: map[string]int{"v0.1.0+x": 2}, releases: map[string]release{"v0.1.0+x": {assets: "0"}},
			event: "workflow_dispatch", ref: "v0.1.0+x", force: "false",
		},
		{
			name: "bad force value", head: 2,
			tags: map[string]int{"v0.1.0": 2}, releases: map[string]release{"v0.1.0": {assets: "0"}},
			event: "workflow_dispatch", ref: "v0.1.0", force: "yes",
		},
		{
			name: "unknown event", head: 2,
			tags: map[string]int{"v0.1.0": 2}, releases: map[string]release{"v0.1.0": {assets: "0"}},
			event: "push", ref: "v0.1.0", force: "false",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir, git, c1, c2 := releaseRepo(t)
			commits := map[int]string{1: c1, 2: c2}
			for tag, n := range c.tags {
				git("tag", tag, commits[n])
			}
			git("checkout", "-q", "--detach", commits[c.head])
			bin, state := stubGH(t)
			for tag, r := range c.releases {
				f, content := filepath.Join(state, tag), r.assets
				if r.err != "" {
					f, content = f+".err", r.err
				}
				if err := os.WriteFile(f, []byte(content+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			ref := map[string]string{"c1": c1, "c2": c2}[c.ref]
			if ref == "" {
				ref = c.ref
			}
			cmd := exec.Command("bash", finder, c.event, ref, c.force)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "STUB_GH_STATE="+state,
				"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			var stdout, stderr strings.Builder
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			if !c.ok {
				if err == nil {
					t.Fatalf("succeeded, want a failure; stdout:\n%s", stdout.String())
				}
				t.Logf("failed as wanted: %v: %s", err, strings.TrimSpace(stderr.String()))
				return
			}
			if err != nil {
				t.Fatalf("%v\nstderr:\n%s", err, stderr.String())
			}
			got := map[string]string{}
			for _, l := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
				k, v, ok := strings.Cut(l, "=")
				if !ok {
					t.Fatalf("output line %q is not key=value", l)
				}
				got[k] = v
			}
			want := maps.Clone(c.want)
			if want["commit"] == "c2" {
				want["commit"] = git("rev-parse", "--short=12", c2)
			}
			if !maps.Equal(got, want) {
				t.Errorf("output = %v, want %v", got, want)
			}
		})
	}
}

// topBlock returns the lines under a top-level YAML key, up to the next
// top-level key, without comments or blank lines.
func topBlock(text, key string) []string {
	var out []string
	in := false
	for _, l := range strings.Split(text, "\n") {
		if l != "" && l[0] != ' ' && l[0] != '#' {
			in = strings.HasPrefix(l, key+":")
			continue
		}
		if in {
			s := strings.TrimSpace(l)
			if s != "" && !strings.HasPrefix(s, "#") {
				out = append(out, s)
			}
		}
	}
	return out
}

// codeLines returns a workflow's lines with comments removed.
func codeLines(text string) []string {
	var out []string
	for _, l := range strings.Split(text, "\n") {
		if s := strings.TrimSpace(l); s != "" && !strings.HasPrefix(s, "#") {
			out = append(out, l)
		}
	}
	return out
}

func readText(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

var usesRE = regexp.MustCompile(`uses:\s*(\S+)@(\S+)`)

// pinnedActions returns action -> SHA for every `uses:` in a workflow.
func pinnedActions(t *testing.T, path, text string) map[string]string {
	t.Helper()
	pins := map[string]string{}
	for _, m := range usesRE.FindAllStringSubmatch(text, -1) {
		if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(m[2]) {
			t.Errorf("%s: %s@%s is not pinned to a commit SHA", path, m[1], m[2])
		}
		if prev, ok := pins[m[1]]; ok && prev != m[2] {
			t.Errorf("%s: %s is pinned to two SHAs", path, m[1])
		}
		pins[m[1]] = m[2]
	}
	return pins
}

func TestReleaseWorkflow(t *testing.T) {
	text := readText(t, releaseWorkflow)
	code := strings.Join(codeLines(text), "\n")

	on := topBlock(text, "on")
	for _, w := range []string{
		"workflow_run:", "workflows: [release-please]", "types: [completed]", "branches: [main]",
		"workflow_dispatch:", "tag:", "force:", "default: false", "type: boolean",
	} {
		if !slices.Contains(on, w) {
			t.Errorf("on: has no %q line", w)
		}
	}
	// A tag or release trigger would never fire for release-please's tag.
	for _, l := range on {
		for _, bad := range []string{"push:", "release:", "pull_request", "tags:"} {
			if strings.HasPrefix(l, bad) {
				t.Errorf("on: holds %q; GITHUB_TOKEN events start no workflow, use workflow_run", l)
			}
		}
	}
	// Any conclusion: a release can exist even if a later release-please step
	// failed. Only a push to main counts.
	for _, w := range []string{
		"github.event.workflow_run.event == 'push'",
		"github.event.workflow_run.head_branch == 'main'",
	} {
		if !strings.Contains(code, w) {
			t.Errorf("%s does not require %s", releaseWorkflow, w)
		}
	}
	if strings.Contains(code, "conclusion") {
		t.Errorf("%s gates on the release-please run's conclusion; the lookup decides", releaseWorkflow)
	}
	// One group per commit or tag, so a newer run never cancels or queues
	// behind the one carrying a release.
	if !strings.Contains(code, "group: ${{ github.workflow }}-${{ github.event.workflow_run.head_sha || inputs.tag }}") {
		t.Errorf("%s's concurrency group is not per head commit or tag", releaseWorkflow)
	}
	if !strings.Contains(code, "cancel-in-progress: false") {
		t.Errorf("%s may cancel a run in progress", releaseWorkflow)
	}

	if perms := topBlock(text, "permissions"); !slices.Equal(perms, []string{"contents: write"}) {
		t.Errorf("permissions = %q, want only contents: write", perms)
	}
	if strings.Count(code, "permissions:") != 1 {
		t.Error("a job widens permissions; only the top-level contents: write is allowed")
	}
	if strings.Contains(code, "secrets.") {
		t.Error("the workflow reads a secret; it must use GITHUB_TOKEN only")
	}

	// Only checkout and setup-go, pinned to the same SHAs as CI.
	pins := pinnedActions(t, releaseWorkflow, code)
	ciPins := pinnedActions(t, ciWorkflow, readText(t, ciWorkflow))
	for _, a := range []string{"actions/checkout", "actions/setup-go"} {
		if pins[a] == "" {
			t.Errorf("%s does not use %s", releaseWorkflow, a)
		} else if pins[a] != ciPins[a] {
			t.Errorf("%s pins %s@%s, CI pins %s", releaseWorkflow, a, pins[a], ciPins[a])
		}
	}
	for a := range pins {
		if a != "actions/checkout" && a != "actions/setup-go" {
			t.Errorf("%s uses %s; no third-party action is allowed", releaseWorkflow, a)
		}
	}

	// The Go version is the spine's release toolchain.
	if goVer := spineReleaseGo(t); !strings.Contains(code, "go-version: '"+goVer+"'") {
		t.Errorf("%s does not set go-version: '%s' (the spine's release toolchain)", releaseWorkflow, goVer)
	}
	if strings.Contains(code, "go-version-file") {
		t.Error("the release toolchain is pinned, not read from go.mod")
	}

	// Find with the script, build with the script, verify every binary, then
	// upload the binaries and SHA256SUMS last.
	steps := []string{
		`scripts/release-find.sh "$EVENT" "$REF" "$FORCE"`,
		"scripts/release-build.sh",
		"sha256sum -c SHA256SUMS",
		`go version -m "$bin"`,
		"_linux_amd64 version",
		`gh release upload "$TAG" herddash_* --clobber`,
		`gh release upload "$TAG" SHA256SUMS --clobber`,
	}
	last := -1
	for _, w := range steps {
		i := strings.Index(code, w)
		if i < 0 {
			t.Errorf("%s does not run %q", releaseWorkflow, w)
			continue
		}
		if i < last {
			t.Errorf("%s runs %q out of order", releaseWorkflow, w)
		}
		last = i
	}
	for _, w := range []string{"GOOS=", "GOARCH=", "CGO_ENABLED=0", "-trimpath=true", "vcs.revision=", "vcs.modified=false"} {
		if !strings.Contains(code, w) {
			t.Errorf("%s's build-info check does not cover %s", releaseWorkflow, w)
		}
	}
	if !strings.Contains(code, "FORCE: ${{ inputs.force && 'true' || 'false' }}") {
		t.Errorf("%s does not pass the force input to release-find.sh as true or false", releaseWorkflow)
	}

	// Neither the workflow nor the finder ever creates a tag or a release.
	for _, path := range []string{releaseWorkflow, releaseFind, releaseScript} {
		for _, l := range codeLines(readText(t, path)) {
			for _, f := range regexp.MustCompile(`gh release (\S+)`).FindAllStringSubmatch(l, -1) {
				if f[1] != "view" && f[1] != "upload" {
					t.Errorf("%s runs gh release %s; only view and upload are allowed", path, f[1])
				}
			}
			if strings.Contains(l, "gh release upload") && path != releaseWorkflow {
				t.Errorf("%s uploads; only the workflow does", path)
			}
			for _, bad := range []string{"git push", "gh api", "create-release", "action-gh-release"} {
				if strings.Contains(l, bad) {
					t.Errorf("%s: %q could create a tag or release", path, strings.TrimSpace(l))
				}
			}
			if strings.Contains(l, "git tag") && !strings.Contains(l, "git tag --points-at") {
				t.Errorf("%s: %q could create a tag", path, strings.TrimSpace(l))
			}
		}
	}
}

func TestReleasePleaseNeverPublishes(t *testing.T) {
	text := readText(t, rpWorkflow)
	code := strings.Join(codeLines(text), "\n")
	if strings.Contains(code, "run:") {
		t.Errorf("%s runs a shell step; it must only run release-please", rpWorkflow)
	}
	pins := pinnedActions(t, rpWorkflow, code)
	if len(pins) != 1 || pins["googleapis/release-please-action"] == "" {
		t.Errorf("%s uses %v, want only the release-please action", rpWorkflow, pins)
	}
	if !strings.Contains(text, "workflow_run") {
		t.Errorf("%s's comment does not name the workflow_run mechanism", rpWorkflow)
	}
	if !strings.Contains(text, "name: release-please\n") {
		t.Errorf("%s is not named release-please, which release.yml's workflow_run names", rpWorkflow)
	}
}

// spineReleaseGo returns the spine's "Go toolchain for releases" version.
func spineReleaseGo(t *testing.T) string {
	t.Helper()
	m := regexp.MustCompile(`\| Go toolchain for releases[^|]*\| (\S+) \|`).FindStringSubmatch(readText(t, spinePath))
	if m == nil {
		t.Fatalf("%s has no release toolchain row", spinePath)
	}
	return m[1]
}

// workflowJobs splits a workflow's jobs: block into job name -> its lines
// (comments and blank lines removed, indentation trimmed), and the job names
// in file order.
func workflowJobs(text string) (map[string][]string, []string) {
	jobs := map[string][]string{}
	var order []string
	jobRE := regexp.MustCompile(`^  ([A-Za-z0-9_-]+):\s*$`)
	in, cur := false, ""
	for _, l := range strings.Split(text, "\n") {
		if l != "" && l[0] != ' ' && l[0] != '#' {
			in, cur = strings.HasPrefix(l, "jobs:"), ""
			continue
		}
		if !in {
			continue
		}
		if m := jobRE.FindStringSubmatch(l); m != nil {
			cur = m[1]
			order = append(order, cur)
			continue
		}
		if s := strings.TrimSpace(l); cur != "" && s != "" && !strings.HasPrefix(s, "#") {
			jobs[cur] = append(jobs[cur], s)
		}
	}
	return jobs, order
}

// TestCIWorkflow checks Story 5.7's CI jobs: the suite runs on macOS with
// plutil required, and the spine's release toolchain vets, tests (with
// systemd-analyze required) and runs the release build on every pull request,
// while the existing jobs keep go.mod's minimum Go.
func TestCIWorkflow(t *testing.T) {
	text := readText(t, ciWorkflow)
	jobs, order := workflowJobs(text)
	want := []string{"check", "board-browser", "macos", "release-toolchain", "cross-compile"}
	if !slices.Equal(order, want) {
		t.Errorf("%s jobs = %q, want %q", ciWorkflow, order, want)
	}

	has := func(job, line string) bool { return slices.Contains(jobs[job], line) }
	contains := func(job, s string) bool {
		return slices.ContainsFunc(jobs[job], func(l string) bool { return strings.Contains(l, s) })
	}

	for _, job := range []string{"check", "board-browser", "cross-compile"} {
		if !has(job, "go-version-file: go.mod") {
			t.Errorf("job %s does not use go.mod's Go (go-version-file: go.mod)", job)
		}
	}

	const testRun = "run: go test -race ./..."
	macosEnv := requireToolsEnv + ": plutil"
	for _, w := range []string{
		"runs-on: macos-latest", "timeout-minutes: 15", "go-version-file: go.mod",
		macosEnv, testRun,
	} {
		if !has("macos", w) {
			t.Errorf("job macos has no %q line", w)
		}
	}

	goVer := spineReleaseGo(t)
	releaseEnv := requireToolsEnv + ": systemd-analyze"
	for _, w := range []string{
		"runs-on: ubuntu-latest", "timeout-minutes: 15", "go-version: '" + goVer + "'",
		"GOTOOLCHAIN: local", "run: go vet ./...", releaseEnv, testRun,
	} {
		if !has("release-toolchain", w) {
			t.Errorf("job release-toolchain has no %q line", w)
		}
	}
	if contains("release-toolchain", "go-version-file") {
		t.Error("job release-toolchain reads go.mod's Go; it must pin the spine's release toolchain")
	}
	// The required-tools variable belongs to the test step itself.
	for job, env := range map[string]string{"macos": macosEnv, "release-toolchain": releaseEnv} {
		lines := jobs[job]
		if i := slices.Index(lines, env); i >= 0 && (i+1 >= len(lines) || lines[i+1] != testRun) {
			t.Errorf("job %s: %q is not immediately followed by %q", job, env, testRun)
		}
	}
	// Vet, then the tests, then the release build, then its outputs checked.
	last := -1
	for _, w := range []string{
		"run: go vet ./...",
		testRun,
		`scripts/release-build.sh 0.0.0-ci "$commit" "$RUNNER_TEMP/dist"`,
		"sha256sum -c SHA256SUMS",
		`[ "$(wc -l < SHA256SUMS)" -eq ` + strconv.Itoa(len(releaseTargets)) + ` ]`,
		"./herddash_0.0.0-ci_linux_amd64 version",
	} {
		i := slices.IndexFunc(jobs["release-toolchain"], func(l string) bool { return strings.Contains(l, w) })
		if i < 0 {
			t.Errorf("job release-toolchain does not run %q", w)
			continue
		}
		if i < last {
			t.Errorf("job release-toolchain runs %q out of order", w)
		}
		last = i
	}

	// Neither new job waits on another, and the workflow uses only checkout
	// and setup-go, one pin each (TestReleaseWorkflow holds those equal to
	// release.yml's).
	for _, job := range []string{"macos", "release-toolchain"} {
		if contains(job, "needs:") {
			t.Errorf("job %s has needs:; it runs beside the others", job)
		}
	}
	for a := range pinnedActions(t, ciWorkflow, strings.Join(codeLines(text), "\n")) {
		if a != "actions/checkout" && a != "actions/setup-go" {
			t.Errorf("%s uses %s; no third-party action is allowed", ciWorkflow, a)
		}
	}
}
