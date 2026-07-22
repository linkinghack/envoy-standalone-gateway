package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/linkinghack/envoy-standalone-gateway/internal/compile"
	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

type conformanceSource struct {
	File     string        `json:"file,omitempty"`
	DocIndex *int          `json:"docIndex,omitempty"`
	Kind     protocol.Kind `json:"kind,omitempty"`
	Name     string        `json:"name,omitempty"`
	Path     string        `json:"path,omitempty"`
}

type conformanceDiagnostic struct {
	Code     string            `json:"code"`
	Stage    string            `json:"stage"`
	Severity string            `json:"severity"`
	Source   conformanceSource `json:"source"`
	Message  string            `json:"message"`
}

type conformanceReport struct {
	Valid       bool                    `json:"valid"`
	Diagnostics []conformanceDiagnostic `json:"diagnostics"`
}

func runConformance(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("conformance", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("f", "", "protocol config directory to validate (required)")
	out := fs.String("o", "", "report file (default: stdout)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		eprintf(stderr, "error: unexpected positional arguments: %s\n", strings.Join(fs.Args(), " "))
		return 2
	}
	if *dir == "" {
		eprintln(stderr, "error: -f <dir> is required")
		return 2
	}

	cs, loadErrs := protocol.LoadDir(*dir)
	diagnostics := make([]conformanceDiagnostic, 0, len(loadErrs))
	for _, item := range loadErrs {
		docIndex := item.Origin.DocIndex
		diagnostics = append(diagnostics, conformanceDiagnostic{
			Code: "ESGW_SCHEMA_INVALID", Stage: string(compile.StageSchema), Severity: "error",
			Source:  conformanceSource{File: item.Origin.File, DocIndex: &docIndex},
			Message: item.Message,
		})
	}
	if len(loadErrs) == 0 {
		_, errs := compile.Compile(cs, compile.Options{Mode: compile.ModeXDS})
		for _, item := range errs {
			severity := "error"
			if item.Severity == compile.SeverityWarning {
				severity = "warning"
			}
			diagnostics = append(diagnostics, conformanceDiagnostic{
				Code: diagnosticCode(item.Stage, item.Severity), Stage: string(item.Stage), Severity: severity,
				Source: conformanceSource{
					File: item.Source.File, Kind: item.Source.Kind, Name: item.Source.Name, Path: item.Source.Path,
				},
				Message: item.Message,
			})
		}
	}
	sort.SliceStable(diagnostics, func(i, j int) bool {
		a, b := diagnostics[i], diagnostics[j]
		ak := a.Stage + "\x00" + a.Source.File + "\x00" + string(a.Source.Kind) + "\x00" + a.Source.Name + "\x00" + a.Source.Path + "\x00" + a.Message
		bk := b.Stage + "\x00" + b.Source.File + "\x00" + string(b.Source.Kind) + "\x00" + b.Source.Name + "\x00" + b.Source.Path + "\x00" + b.Message
		return ak < bk
	})
	valid := true
	for _, item := range diagnostics {
		if item.Severity == "error" {
			valid = false
			break
		}
	}
	report, err := json.MarshalIndent(conformanceReport{Valid: valid, Diagnostics: diagnostics}, "", "  ")
	if err != nil {
		eprintf(stderr, "error: encode conformance report: %v\n", err)
		return 1
	}
	report = append(report, '\n')
	if err := writeArtifact(*out, report, stdout); err != nil {
		eprintf(stderr, "error: %v\n", err)
		return 1
	}
	if !valid {
		return 1
	}
	return 0
}

func diagnosticCode(stage compile.Stage, severity compile.Severity) string {
	suffix := "INVALID"
	if severity == compile.SeverityWarning {
		suffix = "WARNING"
	}
	return fmt.Sprintf("ESGW_%s_%s", strings.ToUpper(string(stage)), suffix)
}
