package config

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
)

// DefaultServer is the Server name of herdr's default socket.
const DefaultServer = "default"

// maxSocketPath is the longest usable unix socket path in bytes: sun_path is
// 104 bytes on darwin, including the terminating NUL.
const maxSocketPath = 103

// target is the resolved herdr target and this Host's identity (AD-3).
type target struct {
	Host       string
	Session    string
	SocketPath string
	Server     string
}

// targetInput is the settings resolveTarget reads, each already picked from
// flag or file.
type targetInput struct {
	host, session, socket, server setting[string]
}

// resolveTarget is the one function that resolves the socket path, Server and
// Host, for serve and import alike (AD-3, AD-8).
//
// The socket path is, in order: --socket or socket_path; --session or session,
// giving <home>/.config/herdr/sessions/<name>/herdr.sock; HERDR_SOCKET_PATH;
// HERDR_SESSION, as for session; and <home>/.config/herdr/herdr.sock.
//
// Server is an explicit server when given. Otherwise it is "default" for the
// default socket and <name> for sessions/<name>/herdr.sock under herdr's
// directory; any other path is an error naming server.
//
// Host is an explicit host when given, else the OS hostname before its first dot.
func resolveTarget(in targetInput, env Env) (target, error) {
	var t target

	host := in.host
	if !host.set {
		short, _, _ := strings.Cut(env.Hostname, ".")
		host = setting[string]{value: short, source: "OS hostname", set: true}
	}
	if err := validateName(host.value); err != nil {
		return target{}, &Error{Key: "host", Source: host.source, Msg: fmt.Sprintf("%q: %v", host.value, err)}
	}
	t.Host = host.value

	// Every supplied name is validated, whether or not it wins precedence.
	envSession := setting[string]{value: env.Vars["HERDR_SESSION"], source: "environment", set: env.Vars["HERDR_SESSION"] != ""}
	for _, n := range []struct {
		key string
		s   setting[string]
	}{{"session", in.session}, {"HERDR_SESSION", envSession}, {"server", in.server}} {
		if !n.s.set {
			continue
		}
		if err := validateName(n.s.value); err != nil {
			return target{}, &Error{Key: n.key, Source: n.s.source, Msg: fmt.Sprintf("%q: %v", n.s.value, err)}
		}
	}

	herdrDir := filepath.Join(env.Home, ".config", "herdr")
	sessionSocket := func(name string) string {
		return filepath.Join(herdrDir, "sessions", name, "herdr.sock")
	}

	var err error
	socketSource := "default"
	switch {
	case in.socket.set:
		socketSource = in.socket.source
		t.SocketPath, err = absPath("socket_path", in.socket, env.Home)
	case in.session.set:
		socketSource = in.session.source
		t.Session = in.session.value
		t.SocketPath = sessionSocket(in.session.value)
	case env.Vars["HERDR_SOCKET_PATH"] != "":
		socketSource = "environment HERDR_SOCKET_PATH"
		t.SocketPath, err = absPath("HERDR_SOCKET_PATH",
			setting[string]{value: env.Vars["HERDR_SOCKET_PATH"], source: "environment", set: true}, env.Home)
	case envSession.set:
		socketSource = "environment HERDR_SESSION"
		t.Session = envSession.value
		t.SocketPath = sessionSocket(envSession.value)
	default:
		t.SocketPath = filepath.Join(herdrDir, "herdr.sock")
	}
	if err != nil {
		return target{}, err
	}
	if len(t.SocketPath) > maxSocketPath {
		return target{}, &Error{Key: "socket_path", Source: socketSource, Msg: fmt.Sprintf(
			"%q is %d bytes; a unix socket path must be at most %d", t.SocketPath, len(t.SocketPath), maxSocketPath)}
	}

	if in.server.set {
		t.Server = in.server.value
		return t, nil
	}
	server, ok := serverFromPath(t.SocketPath, herdrDir)
	if !ok {
		return target{}, &Error{Key: "server", Source: "socket from " + socketSource, Msg: fmt.Sprintf(
			"socket %q is neither herdr's default socket nor a named session's, so server must be set to name this herdr server",
			t.SocketPath)}
	}
	t.Server = server
	return t, nil
}

// serverFromPath derives the Server name from a socket path under herdrDir.
func serverFromPath(socket, herdrDir string) (string, bool) {
	socket = filepath.Clean(socket)
	if socket == filepath.Join(herdrDir, "herdr.sock") {
		return DefaultServer, true
	}
	rel, err := filepath.Rel(filepath.Join(herdrDir, "sessions"), socket)
	if err != nil {
		return "", false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) != 2 || parts[1] != "herdr.sock" || validateName(parts[0]) != nil {
		return "", false
	}
	return parts[0], true
}

// validateName checks a Host, Server or session name. Each becomes a segment
// of API paths (AD-3) or of a socket path, so it must be one non-empty path
// segment without whitespace.
func validateName(s string) error {
	switch {
	case s == "":
		return fmt.Errorf("must be non-empty")
	case s == "." || s == "..":
		return fmt.Errorf("must not be %q", s)
	case strings.ContainsAny(s, "/\\"):
		return fmt.Errorf("must not contain a path separator")
	case strings.ContainsAny(s, "?#%"):
		return fmt.Errorf("must not contain '?', '#' or '%%'")
	case strings.IndexFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0:
		return fmt.Errorf("must not contain whitespace or control characters")
	}
	return nil
}
