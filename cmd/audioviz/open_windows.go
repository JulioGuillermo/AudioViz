package main

import "os/exec"

// openBrowser opens url in the default browser (the URL handler of the shell)
func openBrowser(url string) {
	_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}
