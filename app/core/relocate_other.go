//go:build !windows

package core

func relocateUserPath(oldBase, newBase string, log Logger) {}
