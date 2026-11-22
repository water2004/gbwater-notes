package render

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	merrorNodePattern  = regexp.MustCompile(`data-mml-node="merror"`)
	mjxErrorPattern    = regexp.MustCompile(`data-mjx-error="([^"]+)"`)
	groupTagPattern    = regexp.MustCompile(`<g\b[^>]*>`)
	lineTagPattern     = regexp.MustCompile(`<line\b[^>]*>`)
	strokeWidthPattern = regexp.MustCompile(`\bstroke-width=["']([^"']+)`)
	latexAttrPattern   = regexp.MustCompile(`data-latex="([^"]+)"`)
)

// unknownMathCommands 检测 Folia 用红色 mtext / merror 表达"未知命令"的 SVG，
// 避免把渲染失败的公式当作成功结果发布。
func unknownMathCommands(svg string) []string {
	if merrorNodePattern.MatchString(svg) {
		commands := map[string]bool{}
		for _, match := range mjxErrorPattern.FindAllStringSubmatch(svg, -1) {
			commands[match[1]] = true
		}
		if len(commands) > 0 {
			return sortedKeys(commands)
		}
	}
	commands := map[string]bool{}
	for _, tag := range groupTagPattern.FindAllString(svg, -1) {
		if !strings.Contains(tag, `data-mml-node="mtext"`) {
			continue
		}
		if !strings.Contains(tag, `fill="red"`) && !strings.Contains(tag, `stroke="red"`) {
			continue
		}
		latex := "未知命令"
		if match := latexAttrPattern.FindStringSubmatch(tag); match != nil {
			latex = match[1]
		}
		commands[latex] = true
	}
	return sortedKeys(commands)
}

// invisibleMathLines 找出没有正线宽的数组边框线：节点存在不等于线条可见。
// 数组线由 data-line="v" / "h" 标记，这里先抓全部 <line> 再过滤。
func invisibleMathLines(svg string) []string {
	invisible := []string{}
	for _, tag := range lineTagPattern.FindAllString(svg, -1) {
		if !isArrayLine(tag) {
			continue
		}
		match := strokeWidthPattern.FindStringSubmatch(tag)
		if match == nil {
			invisible = append(invisible, tag)
			continue
		}
		width, err := strconv.ParseFloat(match[1], 64)
		if err != nil || width <= 0 {
			invisible = append(invisible, tag)
		}
	}
	return invisible
}

func isArrayLine(tag string) bool {
	for _, marker := range []string{`data-line="v"`, `data-line="h"`, `data-line='v'`, `data-line='h'`} {
		if strings.Contains(tag, marker) {
			return true
		}
	}
	return false
}

func sortedKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
