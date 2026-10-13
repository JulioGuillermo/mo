package render

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/AlexanderGrooff/mermaid-ascii/pkg/diagram"
	mermaidascii "github.com/AlexanderGrooff/mermaid-ascii/pkg/render"
)

const mmdcTimeout = 30 * time.Second

// plainLabels is the mermaid-cli setting that writes labels as SVG text
// instead of HTML.
const plainLabels = `{"htmlLabels":false,"flowchart":{"htmlLabels":false}}`

// mermaidSVG draws a diagram with mermaid-cli when it is installed.
func mermaidSVG(source string) ([]byte, bool) {
	mmdc, err := exec.LookPath("mmdc")
	if err != nil {
		return nil, false
	}
	dir, err := os.MkdirTemp("", "mo-mermaid-")
	if err != nil {
		return nil, false
	}
	defer os.RemoveAll(dir)
	input, output, settings := filepath.Join(dir, "in.mmd"), filepath.Join(dir, "out.svg"), filepath.Join(dir, "config.json")
	if os.WriteFile(input, []byte(source), 0o600) != nil || os.WriteFile(settings, []byte(plainLabels), 0o600) != nil {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), mmdcTimeout)
	defer cancel()
	if exec.CommandContext(ctx, mmdc, "-i", input, "-o", output, "-c", settings, "-b", "white", "-q").Run() != nil {
		return nil, false
	}
	svg, err := os.ReadFile(output)
	return svg, err == nil
}

// mermaidText draws a flowchart or sequence diagram with box characters.
func mermaidText(source string) (string, error) {
	return mermaidascii.RenderDiagram(source, diagram.DefaultConfig())
}
