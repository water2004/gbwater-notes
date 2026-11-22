import { markdown, markdownLanguage } from '@codemirror/lang-markdown'
import { languages } from '@codemirror/language-data'
import { HighlightStyle } from '@codemirror/language'
import { Tag, tags } from '@lezer/highlight'
import type { MarkdownConfig } from '@lezer/markdown'
import { calloutNames } from './markdown'

const mathTag = Tag.define()
const directiveTag = Tag.define()
const directiveNames = [...Object.keys(calloutNames), 'center', 'minipage']

const noteSyntax: MarkdownConfig = {
  defineNodes: [
    { name: 'NoteMath', style: mathTag },
    { name: 'NoteDirective', style: directiveTag },
  ],
  parseInline: [
    {
      name: 'NoteMath',
      parse(context, next, position) {
        if (next !== 36) return -1
        const source = context.slice(position, context.end)
        const pattern = source.startsWith('$$')
          ? /^\$\$((?:\\[\s\S]|\$(?!\$)|[^$\\])+?)\$\$/
          : /^\$((?:\\[^\n]|[^$\\\n])+?)\$/
        const match = pattern.exec(source)
        if (!match?.[1].trim()) return -1
        return context.addElement(context.elt('NoteMath', position, position + match[0].length))
      },
    },
    {
      name: 'NoteDirective',
      before: 'Link',
      parse(context, next, position) {
        if (next !== 58 && next !== 91) return -1
        const lineStart = context.text.lastIndexOf('\n', position - context.offset - 1) + 1
        if (context.text.slice(lineStart, position - context.offset).trim()) return -1
      const pattern = next === 91 ? /^\[!([a-z][\w-]*)\]/i : /^:::[ \t]*([a-z][\w-]*)?/i
      const match = pattern.exec(context.slice(position, context.end))
        if (!match || (match[1] && !directiveNames.includes(match[1]))) return -1
        return context.addElement(context.elt('NoteDirective', position, position + match[0].trimEnd().length))
      },
    },
  ],
}

export { directiveNames }
export const noteLanguage = markdown({ base: markdownLanguage, extensions: noteSyntax, codeLanguages: languages })

export interface EditorHeading {
  from: number
  to: number
  level: number
  title: string
}

export function findHeadings(source: string): EditorHeading[] {
  const headings: EditorHeading[] = []
  noteLanguage.language.parser.parse(source).iterate({
    enter({ name, from, to, node }) {
      const match = /^(?:ATX|Setext)Heading([1-6])$/.exec(name)
      if (!match) return
      const marks = node.getChildren('HeaderMark')
      const start = name.startsWith('ATX') ? (marks[0]?.to ?? from) : from
      const end = name.startsWith('Setext') || marks.length > 1 ? (marks.at(-1)?.from ?? to) : to
      const title = source.slice(start, end).trim().replace(/\s*\n\s*/g, ' ')
      if (title) headings.push({ from, to: end, level: Number(match[1]), title })
      return false
    },
  })
  return headings
}

export const noteHighlight = HighlightStyle.define([
  { tag: tags.heading, color: 'var(--accent-strong)', fontWeight: '750' },
  { tag: tags.strong, fontWeight: '750' },
  { tag: tags.emphasis, fontStyle: 'italic' },
  { tag: tags.strikethrough, textDecoration: 'line-through' },
  { tag: tags.link, color: 'var(--blue)', textDecoration: 'underline' },
  { tag: tags.url, color: 'var(--text-soft)' },
  { tag: tags.quote, color: 'var(--text-soft)', fontStyle: 'italic' },
  { tag: [tags.processingInstruction, tags.meta, tags.comment], color: 'var(--text-soft)' },
  { tag: [tags.monospace, tags.string, tags.special(tags.string)], color: 'var(--warm)' },
  { tag: [tags.keyword, tags.modifier, tags.controlKeyword, directiveTag], color: 'var(--accent-strong)', fontWeight: '650' },
  { tag: [tags.number, tags.bool, tags.null, tags.regexp, mathTag], color: 'var(--editor-math)' },
  { tag: [tags.typeName, tags.className, tags.function(tags.variableName), tags.tagName, tags.attributeName], color: 'var(--blue)' },
  { tag: [tags.operator, tags.escape], color: 'var(--accent)' },
])
