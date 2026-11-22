//go:build !linux && (!windows || !cgo)

package render

import "errors"

func openMath(string) (mathRenderer, error) {
	return nil, errors.New("当前平台没有 Folia 原生公式渲染器")
}

func openMermaid(string) (mermaidRenderer, error) {
	return nil, errors.New("当前平台没有 Folia 原生 Mermaid 渲染器")
}
