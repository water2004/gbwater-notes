package render

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const mathEm = 18

type mathMode int

const (
	mathInline mathMode = iota
	mathDisplay
	mathStandalone
)

type mathSVG struct {
	source                  string
	width, height, baseline float64
}

type MathFrame struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	Align  float64 `json:"align"`
	Space  float64 `json:"space,omitempty"`
	Break  string  `json:"break,omitempty"`
	Layout int     `json:"layout,omitempty"`
	View   string  `json:"view,omitempty"`
}

type mathPart struct {
	frame MathFrame
	box   [4]float64
	svg   string
	inner string
}

var fullWidthMath = errors.New("Folia 输出了百分比宽度的公式")
var mathBreakWidths = [...]float64{0, .111, .167, .222, .278, .333}

// Match the article container queries in web/src/styles/main.css.
var displayWidths = [...]int{14, 28, 42, 56}

func renderMathLayout(renderer mathRenderer, formula Formula) (string, []MathFrame, error) {
	mode := mathInline
	if formula.Display {
		mode = mathDisplay
	}
	render := func(mode mathMode, width int) ([]mathPart, error) {
		svg, err := renderer.Render(formula.Tex, mode, float64(width)*mathEm)
		if err != nil {
			return nil, err
		}
		if unknown := unknownMathCommands(svg.source); len(unknown) > 0 {
			return nil, fmt.Errorf("公式渲染错误: %s", strings.Join(unknown, ", "))
		}
		return parseMathParts(svg)
	}
	wide := displayWidths[len(displayWidths)-1]
	parts, err := render(mode, wide)
	if errors.Is(err, fullWidthMath) {
		// The standalone renderer supplies a fixed viewport for full-width layouts such as equation tags.
		parts, err = render(mathStandalone, wide)
		mode = mathStandalone
	}
	if err != nil {
		return "", nil, err
	}
	if mode == mathDisplay && len(parts) == 1 && parts[0].frame.Width > float64(displayWidths[0]) {
		part := parts[0]
		parts = nil
		for _, width := range displayWidths {
			layout := part
			if width != wide && part.frame.Width > float64(width) {
				broken, err := render(mathDisplay, width)
				if err != nil {
					return "", nil, err
				}
				if len(broken) != 1 {
					return "", nil, errors.New("Folia 行间公式包含多个 SVG 根节点")
				}
				layout = broken[0]
			}
			layout.frame.Layout = width
			parts = append(parts, layout)
		}
	}
	content, frames := packMath(parts)
	return content, frames, nil
}

func parseMathParts(svg mathSVG) ([]mathPart, error) {
	decoder := xml.NewDecoder(strings.NewReader(svg.source))
	var parts []mathPart
	var pending MathFrame
	for {
		start := decoder.InputOffset()
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		tag, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		attrs := make(map[string]string, len(tag.Attr))
		for _, field := range tag.Attr {
			attrs[field.Name.Local] = field.Value
		}
		switch tag.Name.Local {
		case "mjx-break":
			pending.Break = "soft"
			if attrs["newline"] == "true" {
				pending.Break = "hard"
			}
			if size, err := strconv.Atoi(attrs["size"]); err == nil && size >= 0 && size < len(mathBreakWidths) {
				pending.Space = mathBreakWidths[size]
			} else if value, ok := strings.CutPrefix(attrs["style"], "letter-spacing: "); ok {
				if spacing, err := strconv.ParseFloat(strings.TrimSuffix(value, "em"), 64); err == nil {
					pending.Space = round4(1 + spacing)
				}
			}
			if err := decoder.Skip(); err != nil {
				return nil, err
			}
		case "svg":
			if strings.HasSuffix(attrs["width"], "%") {
				return nil, fullWidthMath
			}
			box, err := mathViewBox(attrs["viewBox"])
			if err != nil {
				return nil, err
			}
			innerStart := decoder.InputOffset()
			if err := decoder.Skip(); err != nil {
				return nil, err
			}
			end := decoder.InputOffset()
			pending.Width, pending.Height = box[2]/1000, box[3]/1000
			pending.Align = round4(-(box[1] + box[3]) / 1000)
			if svg.width > 0 {
				pending.Width, pending.Height = round4(svg.width/mathEm), round4(svg.height/mathEm)
				pending.Align = round4((svg.baseline - svg.height) / mathEm)
			}
			raw := svg.source[start:end]
			inner := strings.TrimSuffix(svg.source[innerStart:end], "</svg>")
			parts = append(parts, mathPart{frame: pending, box: box,
				svg:   latexAttrPattern.ReplaceAllString(raw, ""),
				inner: latexAttrPattern.ReplaceAllString(inner, "")})
			pending = MathFrame{}
		}
	}
	if len(parts) == 0 {
		return nil, errors.New("Folia 未输出公式 SVG")
	}
	return parts, nil
}

func mathViewBox(value string) ([4]float64, error) {
	var box [4]float64
	fields := strings.Fields(value)
	if len(fields) != len(box) {
		return box, errors.New("Folia 公式 SVG 缺少有效的 viewBox")
	}
	for i, field := range fields {
		number, err := strconv.ParseFloat(field, 64)
		if err != nil {
			return box, err
		}
		box[i] = number
	}
	if box[2] <= 0 || box[3] <= 0 {
		return box, errors.New("Folia 公式 SVG 的 viewBox 尺寸无效")
	}
	return box, nil
}

// All views of a formula share one HTTP resource. Each view maps its own
// viewport to the corresponding Folia SVG without exposing TeX in the page.
func packMath(parts []mathPart) (string, []MathFrame) {
	frames := make([]MathFrame, 0, len(parts))
	if len(parts) == 1 {
		return parts[0].svg, append(frames, parts[0].frame)
	}
	var body strings.Builder
	views := map[string]string{}
	y, width := 0.0, 0.0
	for _, part := range parts {
		frame := part.frame
		view, exists := views[part.svg]
		if !exists {
			view = "math-" + strconv.Itoa(len(views)+1)
			views[part.svg] = view
			box := part.box
			fmt.Fprintf(&body, `<view id="%s" viewBox="0 %g %g %g"/><svg x="0" y="%g" width="%g" height="%g" viewBox="%g %g %g %g">%s</svg>`,
				view, y, box[2], box[3], y, box[2], box[3], box[0], box[1], box[2], box[3], part.inner)
			y += box[3]
			width = max(width, box[2])
		}
		frame.View = view
		frames = append(frames, frame)
	}
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %g %g">%s</svg>`, width, y, body.String()), frames
}

func mathMarkup(source string, frames []MathFrame, display bool) string {
	tag, class := "span", "math-inline"
	if display {
		tag, class = "div", "math-display"
	}
	var out strings.Builder
	fmt.Fprintf(&out, `<%s class="%s" role="math" aria-label="公式">`, tag, class)
	for _, frame := range frames {
		if frame.Space > 0 {
			fmt.Fprintf(&out, `<span class="math-gap" style="width:%gem"></span>`, frame.Space)
		}
		switch frame.Break {
		case "soft":
			out.WriteString("<wbr>")
		case "hard":
			out.WriteString("<br>")
		}
		url, class := source, "math-image"
		if frame.View != "" {
			url += "#" + frame.View
		}
		if frame.Layout != 0 {
			class += " math-layout-" + strconv.Itoa(frame.Layout)
		}
		fmt.Fprintf(&out, `<img class="%s" src="%s" alt="" style="width:%gem;aspect-ratio:%g/%g;vertical-align:%gem" loading="lazy" decoding="async">`,
			class, url, frame.Width, frame.Width, frame.Height, frame.Align)
	}
	out.WriteString("</" + tag + ">")
	return out.String()
}
