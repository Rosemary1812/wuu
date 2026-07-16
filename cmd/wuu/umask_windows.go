//go:build windows

package main

// lockProcessUmask is a no-op on Windows: there is no umask concept, and
// files created under %USERPROFILE% inherit ACLs that are already private
// to the current user. securefs owns any further tightening.
func lockProcessUmask() {}
