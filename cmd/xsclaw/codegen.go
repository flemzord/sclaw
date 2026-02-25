package main

import (
	"io"
	"text/template"
)

// CodegenParams controls the generated main.go template.
type CodegenParams struct {
	// FirstPartyPkgs is the list of first-party module import paths to include
	// via blank imports.
	FirstPartyPkgs []string

	// PluginPkgs is the list of third-party plugin import paths to include
	// via blank imports.
	PluginPkgs []string
}

var mainTmpl = template.Must(template.New("main").Parse(`package main

import (
	"fmt"
	"os"

	"github.com/flemzord/sclaw/pkg/app"
{{- range .FirstPartyPkgs}}
	_ "{{.}}"
{{- end}}
{{- range .PluginPkgs}}
	_ "{{.}}"
{{- end}}
)

var (
	version   = "dev"
	commit    = "none"
	date      = "unknown"
	buildHash = ""
)

func main() {
	if err := app.Run(app.RunParams{
		Version:   version,
		Commit:    commit,
		Date:      date,
		BuildHash: buildHash,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
`))

// GenerateMain writes the generated main.go to w.
func GenerateMain(w io.Writer, params CodegenParams) error {
	return mainTmpl.Execute(w, params)
}
