// Package config is wiring (AD-8): one TOML file parsed into one immutable,
// validated struct, flag-over-file-over-default precedence, socket-path discovery,
// and the one function that resolves Host and Server for serve and import alike.
//
// Permitted internal imports: core.
package config
