package studio

import "os"

// drivesPath is the pseudo-folder listing the drives in the file browser
const drivesPath = "::drives"

// fsDrives lists the drives that exist (C:\, D:\...)
func fsDrives() []string {
	var out []string
	for c := 'A'; c <= 'Z'; c++ {
		d := string(c) + `:\`
		if _, err := os.Stat(d); err == nil {
			out = append(out, d)
		}
	}
	return out
}
