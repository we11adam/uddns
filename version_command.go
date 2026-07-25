package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"runtime"
)

type versionInfo struct {
	Version string `json:"version"`
	GOOS    string `json:"goos"`
	GOARCH  string `json:"goarch"`
}

func runVersionCommand(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("uddns version", flag.ContinueOnError)
	flags.SetOutput(stderr)
	jsonOutput := flags.Bool("json", false, "Print version information as JSON")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "uddns version: unexpected argument %q\n", flags.Arg(0))
		return 2
	}

	info := versionInfo{
		Version: version,
		GOOS:    runtime.GOOS,
		GOARCH:  runtime.GOARCH,
	}
	if *jsonOutput {
		if err := json.NewEncoder(stdout).Encode(info); err != nil {
			fmt.Fprintf(stderr, "uddns version: %v\n", err)
			return 1
		}
		return 0
	}
	if _, err := fmt.Fprintf(
		stdout,
		"uddns %s (%s/%s)\n",
		info.Version,
		info.GOOS,
		info.GOARCH,
	); err != nil {
		fmt.Fprintf(stderr, "uddns version: %v\n", err)
		return 1
	}
	return 0
}
