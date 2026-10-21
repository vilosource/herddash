// Package procenv is the adapter that reads one process's environment (AD-14):
// /proc/<pid>/environ on linux, kern.procargs2 on darwin; and a process's
// direct children with their arguments, for the background-task rule:
// /proc on linux, kern.proc.all and kern.procargs2 on darwin.
//
// Permitted internal imports: none.
package procenv
