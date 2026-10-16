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

// textConfig draws boxes tight around their text: room on screen is
// scarce in a terminal. With a width, the boxes move closer to fit it
// where they can.
func textConfig(width ...int) *diagram.Config {
	config := diagram.DefaultConfig()
	config.BoxBorderPadding = 0
	if len(width) > 0 && width[0] > 0 {
		config.MaxWidth = width[0]
	}
	return config
}

// mermaidText draws a flowchart, sequence, ER or class diagram with box
// characters; a class diagram as a flowchart of class boxes.
func mermaidText(source string, width ...int) (string, error) {
	if isClassDiagram(source) {
		flowchart, err := classFlowchart(source)
		if err != nil {
			return "", err
		}
		drawn, err := mermaidascii.RenderDiagram(flowchart, textConfig(width...))
		if err != nil {
			return "", err
		}
		return tidyClassBoxes(drawn), nil
	}
	return mermaidascii.RenderDiagram(readableFlowchart(source), textConfig(width...))
}
