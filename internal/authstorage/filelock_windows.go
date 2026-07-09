//go:build windows

package authstorage

func lockFile(string) (func(), error) { return func() {}, nil }
