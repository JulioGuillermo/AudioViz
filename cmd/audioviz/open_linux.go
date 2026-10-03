package main

import "os/exec"

// openBrowser opens url with the desktop's default handler
func openBrowser(url string) {
	if p, err := exec.LookPath("xdg-open"); err == nil {
		_ = exec.Command(p, url).Start()
	}
}
