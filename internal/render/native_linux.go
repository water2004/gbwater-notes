package render

import (
	"errors"
	"fmt"
	"unicode/utf16"
	"unsafe"

	"github.com/ebitengine/purego"
)

type nativeMath struct {
	lib                uintptr
	renderer, cancel   unsafe.Pointer
	create             func(*unsafe.Pointer) int32
	createCancellation func(*unsafe.Pointer) int32
	render             func(unsafe.Pointer, *uint16, uintptr, int32, float64, float64, unsafe.Pointer, uint32, *unsafe.Pointer) int32
	fragment           func(unsafe.Pointer, *uint16, uintptr, float64, float64, unsafe.Pointer, uint32, *unsafe.Pointer) int32
	svg, detail        func(unsafe.Pointer, *uintptr) *byte
	metrics            func(unsafe.Pointer, *svgMetrics) int32
	freeResult         func(unsafe.Pointer)
	freeRenderer       func(unsafe.Pointer)
	freeCancellation   func(unsafe.Pointer)
}

type svgMetrics struct {
	width, height, baseline float64
}

func openMath(path string) (mathRenderer, error) {
	lib, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, err
	}
	renderer := &nativeMath{lib: lib}
	if err := registerExports(lib, map[string]any{
		"FoliaNativeMathRendererCreate":        &renderer.create,
		"FoliaNativeMathCancellationCreate":    &renderer.createCancellation,
		"FoliaNativeMathRenderSvg":             &renderer.render,
		"FoliaNativeMathRenderSvgFragment":     &renderer.fragment,
		"FoliaNativeMathResultSvg":             &renderer.svg,
		"FoliaNativeMathResultError":           &renderer.detail,
		"FoliaNativeMathResultFragmentMetrics": &renderer.metrics,
		"FoliaNativeMathResultDestroy":         &renderer.freeResult,
		"FoliaNativeMathRendererDestroy":       &renderer.freeRenderer,
		"FoliaNativeMathCancellationDestroy":   &renderer.freeCancellation,
	}); err != nil {
		renderer.Close()
		return nil, err
	}
	if status := renderer.create(&renderer.renderer); status != 0 || renderer.renderer == nil {
		renderer.Close()
		return nil, fmt.Errorf("创建 Folia 数学渲染器失败 (状态 %d)", status)
	}
	if status := renderer.createCancellation(&renderer.cancel); status != 0 || renderer.cancel == nil {
		renderer.Close()
		return nil, fmt.Errorf("创建 Folia 数学取消上下文失败 (状态 %d)", status)
	}
	return renderer, nil
}

func (m *nativeMath) Render(tex string, mode mathMode, width float64) (mathSVG, error) {
	units := utf16.Encode([]rune(tex))
	var result unsafe.Pointer
	var status int32
	if mode == mathStandalone {
		status = m.fragment(m.renderer, unsafe.SliceData(units), uintptr(len(units)), mathEm, width, m.cancel, 15000, &result)
	} else {
		status = m.render(m.renderer, unsafe.SliceData(units), uintptr(len(units)), int32(mode), mathEm, width, m.cancel, 15000, &result)
	}
	if result == nil {
		return mathSVG{}, fmt.Errorf("Folia 未返回公式 (状态 %d)", status)
	}
	defer m.freeResult(result)

	var size uintptr
	if detail := m.detail(result, &size); size != 0 {
		return mathSVG{}, errors.New(nativeText(detail, size))
	}
	if status != 0 {
		return mathSVG{}, fmt.Errorf("Folia 数学渲染状态 %d", status)
	}
	data := m.svg(result, &size)
	svg := mathSVG{source: nativeText(data, size)}
	if svg.source == "" {
		return mathSVG{}, errors.New("Folia 未返回 SVG")
	}
	var metrics svgMetrics
	if mode == mathStandalone {
		if m.metrics(result, &metrics) == 0 {
			return mathSVG{}, errors.New("Folia 未返回公式尺寸")
		}
		svg.width, svg.height, svg.baseline = metrics.width, metrics.height, metrics.baseline
	}
	return svg, nil
}

func (m *nativeMath) Close() {
	if m.cancel != nil {
		m.freeCancellation(m.cancel)
		m.cancel = nil
	}
	if m.renderer != nil {
		m.freeRenderer(m.renderer)
		m.renderer = nil
	}
	if m.lib != 0 {
		_ = purego.Dlclose(m.lib)
		m.lib = 0
	}
}

type nativeMermaid struct {
	lib    uintptr
	render func(*byte, uintptr, bool, **byte, *uintptr, *float32, *float32) int32
	free   func(*byte)
}

func openMermaid(path string) (mermaidRenderer, error) {
	lib, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, err
	}
	renderer := &nativeMermaid{lib: lib}
	if err := registerExports(lib, map[string]any{
		"FoliaNativeMermaidRender": &renderer.render,
		"FoliaNativeMermaidFree":   &renderer.free,
	}); err != nil {
		renderer.Close()
		return nil, err
	}
	return renderer, nil
}

func (m *nativeMermaid) Render(source string, dark bool) (string, float64, float64, error) {
	var output *byte
	var size uintptr
	var width, height float32
	status := m.render(unsafe.StringData(source), uintptr(len(source)), dark, &output, &size, &width, &height)
	if output == nil {
		return "", 0, 0, fmt.Errorf("Folia 未返回 Mermaid 图表 (状态 %d)", status)
	}
	defer m.free(output)
	svg := nativeText(output, size)
	if status != 0 {
		return "", 0, 0, errors.New(svg)
	}
	return svg, float64(width), float64(height), nil
}

func (m *nativeMermaid) Close() {
	if m.lib != 0 {
		_ = purego.Dlclose(m.lib)
		m.lib = 0
	}
}

func registerExports(lib uintptr, exports map[string]any) error {
	for name, target := range exports {
		address, err := purego.Dlsym(lib, name)
		if err != nil {
			return fmt.Errorf("Folia 缺少符号 %s: %w", name, err)
		}
		purego.RegisterFunc(target, address)
	}
	return nil
}

func nativeText(pointer *byte, size uintptr) string {
	if pointer == nil {
		return ""
	}
	return string(unsafe.Slice(pointer, size))
}
