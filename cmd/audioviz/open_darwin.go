package main

import "os/exec"

// openBrowser opens url in the default browser
func openBrowser(url string) { _ = exec.Command("open", url).Start() }
