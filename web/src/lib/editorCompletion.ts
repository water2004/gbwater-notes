import { snippetCompletion, type CompletionSource } from '@codemirror/autocomplete'
import { syntaxTree } from '@codemirror/language'
import { languages } from '@codemirror/language-data'
import type { Text } from '@codemirror/state'
import { calloutNames } from './markdown'
import { directiveNames } from './editorSyntax'

const snippets = [
  ['/heading', '章节标题', '## ${1:标题}'],
  ['/link', '链接', '[${1:文字}](${2:https://})'],
  ['/table', '表格', '| ${1:列一} | ${2:列二} |\n| --- | --- |\n| ${3:内容一} | ${4:内容二} |'],
  ['/code', '代码块', '```${1:text}\n${2:代码}\n```'],
  ['/inline', '行内公式', '$${1:x^2}$'],
  ['/math', '独立公式', '$$\n${1:E = mc^2}\n$$'],
  ['/proof', '证明块', '::: proof\n${1:证明}\n:::'],
  ['/mermaid', 'Mermaid 图表', '```mermaid\nflowchart LR\n  ${1:A --> B}\n```'],
].map(([label, detail, template]) => snippetCompletion(template + '${0}', { label, detail, type: 'text' }))

const directives = directiveNames.map(label => snippetCompletion(
  label + '\n${1:内容}\n:::${0}',
  { label, detail: calloutNames[label] ?? (label === 'center' ? '居中块' : '版面块'), type: 'keyword' },
))
const quoteDirectives = directiveNames.map(label => ({
  label, apply: label + ']', detail: calloutNames[label], type: 'keyword',
}))

const fenceLanguages = [...new Set([...languages.map(language => language.name.toLowerCase()), 'text', 'mermaid'])].map(label => ({
  label, type: 'keyword',
}))

const tex = [
  ['\\frac', '\\frac{${1:分子}}{${2:分母}}'],
  ['\\sqrt', '\\sqrt{${1:表达式}}'],
  ['\\sum', '\\sum_{${1:i=1}}^{${2:n}}'],
  ['\\int', '\\int_{${1:a}}^{${2:b}}'],
  ['\\alpha', '\\alpha'],
  ['\\beta', '\\beta'],
  ['\\lambda', '\\lambda'],
  ['\\mathbf', '\\mathbf{${1:向量}}'],
].map(([label, template]) => snippetCompletion(template + '${0}', { label, type: 'function' }))

function insideDisplayMath(doc: Text, lineNumber: number) {
  let open = false
  for (let number = lineNumber - 1; number > 0; number--) {
    const line = doc.line(number).text.trim()
    if (!line) break
    if (line === '$$') open = !open
  }
  return open
}

export const completeMarkdown: CompletionSource = context => {
  const { state, pos } = context
  const line = state.doc.lineAt(pos)
  const before = state.sliceDoc(line.from, pos)
  let inMath = false
  let inCode = false
  for (let node = syntaxTree(state).resolveInner(pos, -1); node; ) {
    if (node.name === 'NoteMath') inMath = true
    if (node.name === 'FencedCode' || node.name === 'CodeBlock' || node.name === 'InlineCode') inCode = true
    if (!node.parent) break
    node = node.parent
  }
  const fence = /^ {0,3}(?:`{3,}|~{3,})([\w+#.-]*)$/.exec(before)
  if (fence && syntaxTree(state).resolve(pos, -1).name !== 'CodeText') {
    return { from: pos - fence[1].length, options: fenceLanguages, validFor: /^[\w+#.-]*$/ }
  }
  if (inCode) return null
  const math = /\\[a-z]*$/i.exec(before)
  if (math && (inMath || /(?:^|[^\\])\$[^$]*\\[a-z]*$/i.test(before) || insideDisplayMath(state.doc, line.number))) {
    return { from: pos - math[0].length, options: tex, validFor: /^\\[a-z]*$/i }
  }
  if (inMath) return null
  const directive = /^ {0,3}:::[ \t]+([\w-]*)$/.exec(before)
  if (directive) return { from: pos - directive[1].length, options: directives, validFor: /^[\w-]*$/ }
  const quoteDirective = /^ {0,3}(?:>[ \t]*)+\[!([\w-]*)$/.exec(before)
  if (quoteDirective) return {
    from: pos - quoteDirective[1].length,
    to: state.sliceDoc(pos, pos + 1) === ']' ? pos + 1 : pos,
    options: quoteDirectives,
  }
  const slash = /^ {0,3}(\/[a-z]*)$/i.exec(before)
  if (slash) return { from: pos - slash[1].length, options: snippets, validFor: /^\/[a-z]*$/i }
  if (!context.explicit) return null
  const { from, to } = state.selection.main
  return { from, to, options: snippets, filter: false }
}

export const editorPhrases = {
  'Find': '查找', 'Replace': '替换', 'next': '下一处', 'previous': '上一处', 'all': '全选',
  'replace': '替换', 'replace all': '全部替换', 'match case': '区分大小写',
  'regexp': '正则表达式', 'by word': '全词匹配', 'close': '关闭',
  'Control character': '控制字符', 'current match': '当前匹配',
}
