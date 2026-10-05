# Security Policy

## Reporting a vulnerability

Please **do not** open a public GitHub issue for a security vulnerability.

Report it privately via GitHub's private vulnerability reporting on this repository:

1. Go to the [vilosource/herddash](https://github.com/vilosource/herddash) Security tab.
2. Click **Report a vulnerability**.
3. Describe the issue, affected version(s), and reproduction steps if you have them.

This opens a private advisory visible only to maintainers and you, so the issue can be discussed
and fixed before any public disclosure.

## What counts as a vulnerability here

herddash reads the terminal output of your coding agents in order to show what they are doing.
That output routinely contains API tokens, credentials and private source code. Treat anything
that exposes it beyond its intended audience as a vulnerability, in particular:

- Captured pane output reachable without authentication on a non-loopback interface.
- Captured output written to a world-readable path, or into logs.
- A stored event or snapshot surviving longer than the configured retention.
- Any path that lets a browser client reach the herdr socket API directly.

## Supported versions

herddash is pre-1.0. Only the **latest published 0.x minor release** is supported with security
fixes. Older 0.x minors do not receive backports.
