package main

import "runtime"

// exeName is a program's file name on this platform: Windows runs a
// program only by a name that ends in .exe, so a test that builds one and
// starts it by path names it so (the kit's suite on the Windows guest,
// 2026-10-09: "executable file not found in %PATH%").
func exeName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}
