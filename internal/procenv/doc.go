// Package procenv is the adapter that reads one process's environment (AD-14):
// /proc/<pid>/environ on linux, kern.procargs2 on darwin; a process's direct
// children with their arguments, for the background-task rule: /proc on linux,
// kern.proc.all and kern.procargs2 on darwin; and Claude Code's session record
// for one process, <config dir>/sessions/<pid>.json, for its status (ADR-0008).
//
// Permitted internal imports: none.
package procenv
