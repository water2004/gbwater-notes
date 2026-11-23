//go:build windows && cgo

package render

/*
#include <windows.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <stdio.h>

typedef int (*fn_create)(void**);
typedef void (*fn_result_destroy)(void*);
typedef void (*fn_destroy)(void*);
typedef int (*fn_cancel_create)(void**);

typedef struct {
	double width;
	double height;
	double baseline;
} fragment_metrics;

typedef int (*fn_render)(void*, const uint16_t*, size_t, int, double, double, void*, uint32_t, void**);
typedef int (*fn_fragment)(void*, const uint16_t*, size_t, double, double, void*, uint32_t, void**);
typedef const char* (*fn_result_svg)(void*, size_t*);
typedef const char* (*fn_result_error)(void*, size_t*);
typedef int (*fn_result_metrics)(void*, fragment_metrics*);

typedef struct {
	HMODULE module;
	fn_create create;
	fn_cancel_create cancel_create;
	fn_render render;
	fn_fragment fragment;
	fn_result_svg result_svg;
	fn_result_error result_error;
	fn_result_metrics result_metrics;
	fn_result_destroy result_destroy;
	fn_destroy renderer_destroy;
	fn_destroy cancel_destroy;
	void* renderer;
	void* cancellation;
} folia_math;

static void set_error(char* err, int errlen, const char* message) {
	if (err != NULL && errlen > 0) {
		strncpy(err, message, (size_t)errlen - 1);
		err[errlen - 1] = '\0';
	}
}

// folia_math_open 由 C 侧负责 ABI（含浮点参数与 SEH），Go 侧只传指针。
static void* folia_math_open(const wchar_t* path, char* err, int errlen) {
	HMODULE module = LoadLibraryW(path);
	if (module == NULL) {
		set_error(err, errlen, "LoadLibraryW 失败");
		return NULL;
	}
	folia_math* ctx = (folia_math*)calloc(1, sizeof(folia_math));
	if (ctx == NULL) {
		FreeLibrary(module);
		set_error(err, errlen, "内存分配失败");
		return NULL;
	}
	ctx->module = module;
	ctx->create = (fn_create)GetProcAddress(module, "FoliaNativeMathRendererCreate");
	ctx->cancel_create = (fn_cancel_create)GetProcAddress(module, "FoliaNativeMathCancellationCreate");
	ctx->render = (fn_render)GetProcAddress(module, "FoliaNativeMathRenderSvg");
	ctx->fragment = (fn_fragment)GetProcAddress(module, "FoliaNativeMathRenderSvgFragment");
	ctx->result_svg = (fn_result_svg)GetProcAddress(module, "FoliaNativeMathResultSvg");
	ctx->result_error = (fn_result_error)GetProcAddress(module, "FoliaNativeMathResultError");
	ctx->result_metrics = (fn_result_metrics)GetProcAddress(module, "FoliaNativeMathResultFragmentMetrics");
	ctx->result_destroy = (fn_result_destroy)GetProcAddress(module, "FoliaNativeMathResultDestroy");
	ctx->renderer_destroy = (fn_destroy)GetProcAddress(module, "FoliaNativeMathRendererDestroy");
	ctx->cancel_destroy = (fn_destroy)GetProcAddress(module, "FoliaNativeMathCancellationDestroy");
	if (ctx->create == NULL || ctx->cancel_create == NULL || ctx->render == NULL || ctx->fragment == NULL ||
		ctx->result_svg == NULL || ctx->result_error == NULL || ctx->result_metrics == NULL ||
		ctx->result_destroy == NULL || ctx->renderer_destroy == NULL || ctx->cancel_destroy == NULL) {
		set_error(err, errlen, "FoliaNativeMath 导出函数不完整");
		FreeLibrary(module);
		free(ctx);
		return NULL;
	}
	if (ctx->create(&ctx->renderer) != 0 || ctx->renderer == NULL) {
		set_error(err, errlen, "无法创建 Folia 数学渲染器");
		FreeLibrary(module);
		free(ctx);
		return NULL;
	}
	if (ctx->cancel_create(&ctx->cancellation) != 0 || ctx->cancellation == NULL) {
		ctx->renderer_destroy(ctx->renderer);
		set_error(err, errlen, "无法创建 Folia 数学取消上下文");
		FreeLibrary(module);
		free(ctx);
		return NULL;
	}
	return ctx;
}

// folia_math_render 返回 0 表示成功，1 表示失败（错误写入 err）。
static int folia_math_render(void* handle, const uint16_t* tex, size_t length, int mode, double font_size,
		double max_width, char** svg, size_t* svg_length, double* width, double* height,
		double* baseline, char* err, int errlen) {
	folia_math* ctx = (folia_math*)handle;
	void* result = NULL;
	int status = mode == 2
		? ctx->fragment(ctx->renderer, tex, length, font_size, max_width, ctx->cancellation, 15000, &result)
		: ctx->render(ctx->renderer, tex, length, mode, font_size, max_width, ctx->cancellation, 15000, &result);
	if (result == NULL) {
		snprintf(err, (size_t)errlen, "Folia 未返回结果 (状态 %d)", status);
		return 1;
	}
	size_t error_size = 0;
	const char* error_text = ctx->result_error(result, &error_size);
	if (error_text != NULL && error_size > 0) {
		if (err != NULL && errlen > 0) {
			size_t copy = error_size < (size_t)errlen - 1 ? error_size : (size_t)errlen - 1;
			memcpy(err, error_text, copy);
			err[copy] = '\0';
		}
		ctx->result_destroy(result);
		return 1;
	}
	if (status != 0) {
		snprintf(err, (size_t)errlen, "Folia 数学渲染状态 %d", status);
		ctx->result_destroy(result);
		return 1;
	}
	size_t source_length = 0;
	const char* source = ctx->result_svg(result, &source_length);
	if (source == NULL) {
		set_error(err, errlen, "Folia 未返回 SVG");
		ctx->result_destroy(result);
		return 1;
	}
	char* copy = (char*)malloc(source_length + 1);
	if (copy == NULL) {
		set_error(err, errlen, "内存分配失败");
		ctx->result_destroy(result);
		return 1;
	}
	memcpy(copy, source, source_length);
	copy[source_length] = '\0';
	fragment_metrics metrics = {0, 0, 0};
	if (mode == 2 && !ctx->result_metrics(result, &metrics)) {
		free(copy);
		set_error(err, errlen, "Folia 未返回公式尺寸");
		ctx->result_destroy(result);
		return 1;
	}
	*svg = copy;
	*svg_length = source_length;
	*width = metrics.width;
	*height = metrics.height;
	*baseline = metrics.baseline;
	ctx->result_destroy(result);
	return 0;
}

static void folia_math_close(void* handle) {
	folia_math* ctx = (folia_math*)handle;
	if (ctx == NULL) {
		return;
	}
	if (ctx->cancellation != NULL) {
		ctx->cancel_destroy(ctx->cancellation);
	}
	if (ctx->renderer != NULL) {
		ctx->renderer_destroy(ctx->renderer);
	}
	FreeLibrary(ctx->module);
	free(ctx);
}
*/
import "C"

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"github.com/ebitengine/purego"
	"golang.org/x/sys/windows"
)

type windowsMath struct {
	handle unsafe.Pointer
}

func openMath(dllPath string) (mathRenderer, error) {
	prepareDLLDirectories(filepath.Dir(dllPath))
	utf16Path, err := syscall.UTF16FromString(dllPath)
	if err != nil {
		return nil, err
	}
	errBuffer := make([]byte, 512)
	handle := C.folia_math_open(
		(*C.wchar_t)(unsafe.Pointer(&utf16Path[0])),
		(*C.char)(unsafe.Pointer(&errBuffer[0])),
		C.int(len(errBuffer)),
	)
	if handle == nil {
		return nil, fmt.Errorf("加载数学渲染器失败: %s", cString(errBuffer))
	}
	return &windowsMath{handle: handle}, nil
}

func (m *windowsMath) Render(tex string, mode mathMode, maxWidth float64) (mathSVG, error) {
	units := utf16Encode(tex)
	var textPointer *C.uint16_t
	if len(units) > 0 {
		textPointer = (*C.uint16_t)(unsafe.Pointer(&units[0]))
	}
	errBuffer := make([]byte, 512)
	var svgPointer *C.char
	var svgLength C.size_t
	var width, height, baseline C.double
	status := C.folia_math_render(
		m.handle, textPointer, C.size_t(len(units)), C.int(mode), C.double(mathEm), C.double(maxWidth),
		&svgPointer, &svgLength, &width, &height, &baseline,
		(*C.char)(unsafe.Pointer(&errBuffer[0])), C.int(len(errBuffer)),
	)
	if status != 0 {
		return mathSVG{}, errors.New(cString(errBuffer))
	}
	svg := C.GoStringN(svgPointer, C.int(svgLength))
	C.free(unsafe.Pointer(svgPointer))
	return mathSVG{source: svg, width: float64(width), height: float64(height), baseline: float64(baseline)}, nil
}

func (m *windowsMath) Close() {
	if m.handle != nil {
		C.folia_math_close(m.handle)
		m.handle = nil
	}
}

type windowsMermaid struct {
	lib    *syscall.DLL
	render func(*byte, uintptr, uint8, *unsafe.Pointer, *uintptr, *float32, *float32) int32
	free   func(uintptr)
}

func openMermaid(dllPath string) (mermaidRenderer, error) {
	directories := []string{filepath.Dir(dllPath)}
	if mathDLL := os.Getenv("FOLIA_MATH_DLL"); mathDLL != "" {
		directories = append(directories, filepath.Dir(mathDLL))
	}
	prepareDLLDirectories(directories...)
	lib, err := syscall.LoadDLL(dllPath)
	if err != nil {
		return nil, fmt.Errorf("加载 Mermaid 渲染器失败: %w", err)
	}
	renderer := &windowsMermaid{lib: lib}
	renderProc, err := lib.FindProc("FoliaNativeMermaidRender")
	if err != nil {
		lib.Release()
		return nil, fmt.Errorf("FoliaNativeMermaid 缺少渲染函数: %w", err)
	}
	freeProc, err := lib.FindProc("FoliaNativeMermaidFree")
	if err != nil {
		lib.Release()
		return nil, fmt.Errorf("FoliaNativeMermaid 缺少释放函数: %w", err)
	}
	purego.RegisterFunc(&renderer.render, renderProc.Addr())
	purego.RegisterFunc(&renderer.free, freeProc.Addr())
	return renderer, nil
}

func (m *windowsMermaid) Render(source string, dark bool) (string, float64, float64, error) {
	data := []byte(source)
	var pointer *byte
	if len(data) > 0 {
		pointer = &data[0]
	}
	var output unsafe.Pointer
	var outputLength uintptr
	var width, height float32
	darkFlag := uint8(0)
	if dark {
		darkFlag = 1
	}
	status := m.render(pointer, uintptr(len(data)), darkFlag, &output, &outputLength, &width, &height)
	if output == nil {
		return "", 0, 0, fmt.Errorf("Folia Mermaid 渲染状态: %d", status)
	}
	defer m.free(uintptr(output))
	svg := goBytes(output, outputLength)
	if status != 0 {
		return "", 0, 0, errors.New(svg)
	}
	return svg, float64(width), float64(height), nil
}

func (m *windowsMermaid) Close() {
	if m.lib != nil {
		m.lib.Release()
		m.lib = nil
	}
}

func goBytes(pointer unsafe.Pointer, size uintptr) string {
	if pointer == nil || size == 0 {
		return ""
	}
	return string(unsafe.Slice((*byte)(pointer), int(size)))
}

func utf16Encode(value string) []uint16 { return utf16.Encode([]rune(value)) }

func cString(buffer []byte) string {
	for index, value := range buffer {
		if value == 0 {
			return string(buffer[:index])
		}
	}
	return string(buffer)
}

func prepareDLLDirectories(directories ...string) {
	_ = windows.SetDefaultDllDirectories(windows.LOAD_LIBRARY_SEARCH_DEFAULT_DIRS | windows.LOAD_LIBRARY_SEARCH_USER_DIRS)
	cleaned := make([]string, 0, len(directories))
	seen := map[string]bool{}
	for _, directory := range directories {
		if directory == "" || seen[strings.ToLower(directory)] {
			continue
		}
		seen[strings.ToLower(directory)] = true
		cleaned = append(cleaned, directory)
		if pointer, err := windows.UTF16PtrFromString(directory); err == nil {
			_, _ = windows.AddDllDirectory(pointer)
		}
	}
	if len(cleaned) == 0 {
		return
	}
	if current := os.Getenv("PATH"); current != "" {
		cleaned = append(cleaned, current)
	}
	_ = os.Setenv("PATH", strings.Join(cleaned, string(os.PathListSeparator)))
}
