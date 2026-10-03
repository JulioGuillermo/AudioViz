//go:build !windows

package studio

// drivesPath is only used on Windows (list of drives); elsewhere everything hangs from "/"
const drivesPath = "::drives"

func fsDrives() []string { return nil }
