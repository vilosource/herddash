// Package procenv is the adapter that reads one process's environment (AD-14):
// /proc/<pid>/environ on linux, kern.procargs2 on darwin.
//
// Permitted internal imports: none.
package procenv
