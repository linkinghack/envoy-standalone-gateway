package main

import (
	"flag"
	"io"
	"strings"

	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

func runSchema(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("schema", flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := fs.String("o", "", "output file (default: stdout)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		eprintf(stderr, "error: unexpected positional arguments: %s\n", strings.Join(fs.Args(), " "))
		return 2
	}
	bundle, err := protocol.Schemas()
	if err != nil {
		eprintf(stderr, "error: generate schema: %v\n", err)
		return 1
	}
	bundle = append(bundle, '\n')
	if err := writeArtifact(*out, bundle, stdout); err != nil {
		eprintf(stderr, "error: %v\n", err)
		return 1
	}
	return 0
}
