// Package sysbin is the single list of macOS system tools this client
// shells out to, by absolute path. Never a bare command name: much of this
// runs under privilege.Elevate (effective root) in a setuid binary, and a
// bare name would be resolved via $PATH, which the invoking user fully
// controls — classic setuid PATH hijacking.
package sysbin

const (
	Route        = "/sbin/route"
	Ifconfig     = "/sbin/ifconfig"
	Ping         = "/sbin/ping"
	Networksetup = "/usr/sbin/networksetup"
	Scutil       = "/usr/sbin/scutil"
	Security     = "/usr/bin/security"
	Tail         = "/usr/bin/tail"
	Dscacheutil  = "/usr/bin/dscacheutil"
	Killall      = "/usr/bin/killall"
)
