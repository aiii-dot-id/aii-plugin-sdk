package main

import (
	"flag"
	"fmt"
	"os"
	"runtime/debug"
	"strings"
)

func cmdVersion(args []string) int {
	fs := flag.NewFlagSet("aiisdk version", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: aiisdk version

Prints, on one line, the module this aiisdk was built from, its version
and the commit it was built at, as the Go toolchain recorded them in the
binary. A release installed with 'go install ...@vX.Y.Z' carries the
version and no commit (its version names one); a build made in a
checkout carries both, and whether the checkout had changes. What the
record lacks is printed as "unknown", with the reason.
`)
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "aiisdk version: takes no arguments, got %q\n", fs.Args())
		return 2
	}
	fmt.Println(versionLine(debug.ReadBuildInfo()))
	return 0
}

func versionLine(bi *debug.BuildInfo, ok bool) string {
	if !ok || bi == nil {
		return "aiisdk version=unknown revision=unknown (this binary carries no build information)"
	}
	var why []string
	version := bi.Main.Version
	if version == "" {
		version = "unknown"
		why = append(why, "the build recorded no module version")
	}
	var revision, modified string
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			modified = s.Value
		}
	}
	if revision == "" {
		revision = "unknown"
		why = append(why, "Go stamps a revision only into a go build or go install run inside a checkout, without -buildvcs=false")
	} else if modified != "" {
		revision += " modified=" + modified
	}
	line := fmt.Sprintf("aiisdk %s version=%s revision=%s", bi.Main.Path, version, revision)
	if len(why) > 0 {
		line += " (" + strings.Join(why, "; ") + ")"
	}
	return line
}
