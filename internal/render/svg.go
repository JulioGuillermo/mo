package render

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	resvg "github.com/kanrichan/resvg-go"
)

const (
	defaultSVGWidth  = 800
	defaultSVGHeight = 600
	svgNamespace     = "http://www.w3.org/2000/svg"
)

var fontFiles = []struct {
	family string
	paths  []string
}{
	{"Liberation Sans", []string{"liberation/LiberationSans-Regular.ttf", "liberation/LiberationSans-Bold.ttf", "truetype/liberation/LiberationSans-Regular.ttf", "truetype/liberation/LiberationSans-Bold.ttf"}},
	{"DejaVu Sans", []string{"TTF/DejaVuSans.ttf", "TTF/DejaVuSans-Bold.ttf", "truetype/dejavu/DejaVuSans.ttf", "truetype/dejavu/DejaVuSans-Bold.ttf"}},
	{"Noto Sans", []string{"noto/NotoSans-Regular.ttf", "noto/NotoSans-Bold.ttf", "truetype/noto/NotoSans-Regular.ttf"}},
}

var fontRoots = []string{"/usr/share/fonts", "/usr/local/share/fonts", "/Library/Fonts", "/System/Library/Fonts/Supplemental"}

var (
	fontFamilyAttribute = regexp.MustCompile(`font-family=("[^"]*"|'[^']*')`)
	styleAttribute      = regexp.MustCompile(`style=("[^"]*"|'[^']*')`)
	styleElement        = regexp.MustCompile(`(?s)(<style[^>]*>)(.*?)(</style>)`)
	fontFamilyProperty  = regexp.MustCompile(`font-family\s*:\s*`)
	svgOpenTag          = regexp.MustCompile(`<svg\b[^>]*>`)
	sizeAttributes      = regexp.MustCompile(`\s(width|height)\s*=\s*("[^"]*"|'[^']*')`)
	lengthValue         = regexp.MustCompile(`^([0-9.eE+-]+)\s*([a-z]*)$`)
	svgMu               sync.Mutex
)

type svgEngine struct {
	renderer *resvg.Renderer
	family   string
}

// engine starts resvg, compiled to WebAssembly, once, on first use, with the
// first of fontFiles that is installed.
var engine = sync.OnceValues(func() (svgEngine, error) {
	worker, err := resvg.NewContext(context.Background())
	if err != nil {
		return svgEngine{}, err
	}
	renderer, err := worker.NewRenderer()
	if err != nil {
		return svgEngine{}, err
	}
	family := loadFonts(renderer)
	if family != "" {
		renderer.SetFontFamily(family)
	}
	return svgEngine{renderer: renderer, family: family}, nil
})

// rasterizeSVG draws an SVG to fit maxWidth x maxHeight pixels, keeping its
// proportions. Malformed SVG is refused before resvg sees it.
func rasterizeSVG(svg []byte, maxWidth, maxHeight int) (image.Image, error) {
	sized, width, height, err := normalizeSVG(svg)
	if err != nil {
		return nil, err
	}
	scale := min(float64(maxWidth)/width, float64(maxHeight)/height)
	svgEngine, err := engine()
	if err != nil {
		return nil, fmt.Errorf("svg renderer: %w", err)
	}
	svgMu.Lock()
	png, err := svgEngine.renderer.RenderWithSize(withInstalledFont(sized, svgEngine.family), uint32(max(1, width*scale)), uint32(max(1, height*scale)))
	svgMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("svg: %w", err)
	}
	return Decode(png)
}

// normalizeSVG checks the SVG is well-formed XML with an svg root in the SVG
// namespace, adding the namespace when it is missing, and gives the root an
// absolute width and height, so resvg and mo agree on its size.
func normalizeSVG(svg []byte) (normalized []byte, width, height float64, err error) {
	root, err := svgRoot(svg)
	if err != nil {
		return nil, 0, 0, err
	}
	attributes := map[string]string{}
	for _, attribute := range root.Attr {
		if attribute.Name.Space == "" {
			attributes[attribute.Name.Local] = attribute.Value
		}
	}
	width, height = svgSize(attributes)
	tag := svgOpenTag.Find(svg)
	newTag := sizeAttributes.ReplaceAll(tag, nil)
	newTag = append(newTag[:len(newTag)-1], []byte(fmt.Sprintf(` width="%g" height="%g">`, width, height))...)
	if root.Name.Space == "" {
		newTag = append([]byte(`<svg xmlns="`+svgNamespace+`"`), newTag[len("<svg"):]...)
	}
	return bytes.Replace(svg, tag, newTag, 1), width, height, nil
}

func svgRoot(svg []byte) (xml.StartElement, error) {
	decoder := xml.NewDecoder(bytes.NewReader(svg))
	var root *xml.StartElement
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return xml.StartElement{}, fmt.Errorf("svg is not well-formed: %w", err)
		}
		if start, ok := token.(xml.StartElement); ok && root == nil {
			root = &start
		}
	}
	switch {
	case root == nil || root.Name.Local != "svg":
		return xml.StartElement{}, errors.New("not an svg document")
	case root.Name.Space != "" && root.Name.Space != svgNamespace:
		return xml.StartElement{}, fmt.Errorf("svg in namespace %q", root.Name.Space)
	case svgOpenTag.Find(svg) == nil:
		return xml.StartElement{}, errors.New("svg root tag not found")
	}
	return *root, nil
}

// svgSize takes the proportions from the viewBox when there is one, scaled
// to an absolute width or height when one is given; % and em have no size
// of their own and are ignored.
func svgSize(attributes map[string]string) (width, height float64) {
	width, widthKnown := absoluteLength(attributes["width"])
	height, heightKnown := absoluteLength(attributes["height"])
	box := strings.Fields(strings.ReplaceAll(attributes["viewBox"], ",", " "))
	if len(box) == 4 {
		boxWidth, _ := strconv.ParseFloat(box[2], 64)
		boxHeight, _ := strconv.ParseFloat(box[3], 64)
		if boxWidth > 0 && boxHeight > 0 {
			switch {
			case widthKnown:
				return width, width * boxHeight / boxWidth
			case heightKnown:
				return height * boxWidth / boxHeight, height
			}
			return boxWidth, boxHeight
		}
	}
	if widthKnown && heightKnown {
		return width, height
	}
	return defaultSVGWidth, defaultSVGHeight
}

var pixelsPerUnit = map[string]float64{"": 1, "px": 1, "pt": 4.0 / 3, "pc": 16, "mm": 96 / 25.4, "cm": 96 / 2.54, "in": 96}

func absoluteLength(value string) (float64, bool) {
	match := lengthValue.FindStringSubmatch(strings.TrimSpace(value))
	if match == nil {
		return 0, false
	}
	perUnit, absolute := pixelsPerUnit[match[2]]
	number, err := strconv.ParseFloat(match[1], 64)
	if !absolute || err != nil || number <= 0 || math.IsInf(number, 0) {
		return 0, false
	}
	return number * perUnit, true
}

// withInstalledFont puts the installed family first in every font-family:
// the attribute, the CSS property in style attributes, and in style
// elements.
func withInstalledFont(svg []byte, family string) []byte {
	if family == "" {
		return svg
	}
	svg = fontFamilyAttribute.ReplaceAllFunc(svg, func(attribute []byte) []byte {
		quote, value := splitQuoted(attribute[len("font-family="):])
		return []byte(fmt.Sprintf("font-family=%c%s, %s%c", quote, quotedFamily(family, quote), value, quote))
	})
	svg = styleAttribute.ReplaceAllFunc(svg, func(attribute []byte) []byte {
		quote, value := splitQuoted(attribute[len("style="):])
		value = fontFamilyProperty.ReplaceAll(value, []byte("font-family: "+quotedFamily(family, quote)+", "))
		return []byte(fmt.Sprintf("style=%c%s%c", quote, value, quote))
	})
	return styleElement.ReplaceAllFunc(svg, func(element []byte) []byte {
		parts := styleElement.FindSubmatch(element)
		css := fontFamilyProperty.ReplaceAll(parts[2], []byte(`font-family: "`+family+`", `))
		return append(append(append([]byte{}, parts[1]...), css...), parts[3]...)
	})
}

func splitQuoted(quoted []byte) (byte, []byte) {
	return quoted[0], quoted[1 : len(quoted)-1]
}

// quotedFamily quotes the family with the quote the surrounding attribute
// does not use.
func quotedFamily(family string, attributeQuote byte) string {
	if attributeQuote == '\'' {
		return `"` + family + `"`
	}
	return "'" + family + "'"
}

func loadFonts(renderer *resvg.Renderer) string {
	for _, font := range fontFiles {
		loaded := false
		for _, root := range fontRoots {
			for _, path := range font.paths {
				data, err := os.ReadFile(filepath.Join(root, path))
				if err == nil && renderer.LoadFontData(data) == nil {
					loaded = true
				}
			}
		}
		if loaded {
			return font.family
		}
	}
	return ""
}

// SVGPoint is where in an SVG's own units, as its viewBox counts them, a
// point of its picture falls: fx and fy a fraction across and down the
// cols × rows cells it was drawn in, proportions kept and centred, as
// SVGCells or SVGKitty draw it. inside is unset in the margin around it.
func SVGPoint(svg []byte, cols, rows int, kitty bool, fx, fy float64) (x, y float64, inside bool) {
	root, err := svgRoot(svg)
	if err != nil || cols <= 0 || rows <= 0 {
		return 0, 0, false
	}
	attributes := map[string]string{}
	for _, attribute := range root.Attr {
		if attribute.Name.Space == "" {
			attributes[attribute.Name.Local] = attribute.Value
		}
	}
	width, height := svgSize(attributes)
	minX, minY, boxWidth, boxHeight := 0.0, 0.0, width, height
	if box := strings.Fields(strings.ReplaceAll(attributes["viewBox"], ",", " ")); len(box) == 4 {
		values := make([]float64, 4)
		for i, field := range box {
			values[i], _ = strconv.ParseFloat(field, 64)
		}
		if values[2] > 0 && values[3] > 0 {
			minX, minY, boxWidth, boxHeight = values[0], values[1], values[2], values[3]
		}
	}
	cellWidth, cellHeight := svgCellScale, 2*svgCellScale
	if kitty {
		cellWidth, cellHeight = CellSize()
	}
	pixelWidth, pixelHeight := float64(cols*cellWidth), float64(rows*cellHeight)
	scale := math.Min(pixelWidth/boxWidth, pixelHeight/boxHeight)
	left, top := (pixelWidth-boxWidth*scale)/2, (pixelHeight-boxHeight*scale)/2
	x = minX + (fx*pixelWidth-left)/scale
	y = minY + (fy*pixelHeight-top)/scale
	inside = x >= minX && x <= minX+boxWidth && y >= minY && y <= minY+boxHeight
	return x, y, inside
}
