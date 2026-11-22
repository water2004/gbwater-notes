<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { basicSetup } from 'codemirror'
import { Compartment, EditorState, Prec } from '@codemirror/state'
import { EditorView, keymap, placeholder, tooltips } from '@codemirror/view'
import { acceptCompletion, autocompletion, completionStatus, startCompletion } from '@codemirror/autocomplete'
import { openSearchPanel } from '@codemirror/search'
import { syntaxHighlighting } from '@codemirror/language'
import type { ImageTarget } from '@/lib/editorImages'
import { noteHighlight, noteLanguage } from '@/lib/editorSyntax'
import { completeMarkdown, editorPhrases } from '@/lib/editorCompletion'

export type MarkupAction = 'bold' | 'italic' | 'strike' | 'code' | 'link' | 'heading' | 'quote' | 'unordered' | 'ordered' | 'codeBlock' | 'inlineMath' | 'displayMath' | 'callout' | 'table' | 'divider'

const props = defineProps<{ modelValue: string; placeholder?: string; readOnly?: boolean }>()
const emit = defineEmits<{
  'update:modelValue': [value: string]
  command: [command: 'bold' | 'italic' | 'save']
  selection: [value: { line: number; column: number; offset: number }]
  scroll: [ratio: number]
  images: [files: File[], target: ImageTarget]
}>()
const host = ref<HTMLElement>()
const editable = new Compartment()
let view: EditorView | undefined
const tracked = new Set<{ from: number; to: number; original?: string }>()

function imageFiles(files?: FileList | null) {
  return Array.from(files ?? []).filter(file => file.type.startsWith('image/') || /\.(png|jpe?g|gif|webp)$/i.test(file.name))
}

function captureImageTarget(range?: { from: number; to: number }): ImageTarget | undefined {
  if (!view || props.readOnly) return
  const point = view.state.selection.main.to
  const target = range ? { ...range, original: view.state.doc.sliceString(range.from, range.to) } : { from: point, to: point, original: undefined }
  tracked.add(target)
  return {
    apply(content, focus = false) {
      if (!view || props.readOnly || !tracked.has(target)) return false
      const { from, to, original } = target
      if (original !== undefined && (from > to || view.state.doc.sliceString(from, to) !== original)) return false
      const before = from > 0 && view.state.doc.sliceString(from - 1, from) !== '\n' ? '\n' : ''
      const insert = original === undefined ? before + content + '\n' : content
      const selection = view.state.selection.main
      const follow = focus || (original === undefined && selection.empty && selection.head === from && view.hasFocus)
      view.dispatch({
        changes: { from, to, insert },
        selection: follow ? { anchor: from + insert.length } : undefined,
        scrollIntoView: follow,
        userEvent: original === undefined ? 'input' : 'input.replace',
      })
      if (original === undefined) target.from = target.to = from + insert.length
      else tracked.delete(target)
      if (focus) view.focus()
      return true
    },
    release: () => { tracked.delete(target) },
  }
}

function selectSource(from: number, to: number) {
  if (!view) return
  view.dispatch({ selection: { anchor: from, head: to }, scrollIntoView: true })
  view.focus()
}

function removeImageReferences(references: { from: number; to: number }[]) {
  if (!view || !references.length) return
  view.dispatch({
    changes: references.map(({ from, to }) => ({ from, to, insert: '' })),
    userEvent: 'delete.image',
  })
}

const editorTheme = EditorView.theme({
  '&': { height: '100%', backgroundColor: 'transparent', color: 'var(--text)', fontSize: '14px' },
  '.cm-content': { padding: '16px 0 48px', caretColor: 'var(--accent)', fontFamily: 'var(--mono)', lineHeight: '1.8' },
  '.cm-line': { padding: '0 12px' },
  '.cm-gutters': { minWidth: '44px', borderRight: '1px solid var(--line)', backgroundColor: 'var(--surface)', color: 'var(--text-soft)', fontFamily: 'var(--mono)' },
  '.cm-activeLine, .cm-activeLineGutter': { backgroundColor: 'color-mix(in srgb, var(--accent-soft) 52%, transparent)' },
  '.cm-activeLineGutter': { color: 'var(--accent-strong)' },
  '.cm-cursor': { borderLeftColor: 'var(--accent)' },
  '&.cm-focused': { outline: 'none' },
  '.cm-selectionBackground, &.cm-focused .cm-selectionBackground': { backgroundColor: 'color-mix(in srgb, var(--accent) 20%, transparent) !important' },
  '.cm-scroller': { fontFamily: 'var(--mono)', overflow: 'auto' },
  '.cm-placeholder': { color: 'var(--text-faint)' },
  '.cm-panels': { borderColor: 'var(--line)', backgroundColor: 'var(--surface)', color: 'var(--text)' },
  '.cm-search': { padding: '8px 36px 8px 10px !important' },
  '.cm-search input[type=text], .cm-search button': { minHeight: '32px', border: '1px solid var(--line-strong)', borderRadius: '6px', background: 'var(--surface-raised)', color: 'var(--text)', fontFamily: 'var(--sans)', fontSize: '13px' },
  '.cm-search input[type=text]': { width: 'min(215px, 90%)', padding: '4px 8px' },
  '.cm-search label': { display: 'inline-flex', alignItems: 'center', color: 'var(--text-soft)', fontSize: '12px' },
  '.cm-search button[name=close]': { minWidth: '28px', right: '8px', top: '8px' },
  '.cm-searchMatch': { backgroundColor: 'var(--warm-soft)', outline: '1px solid var(--warm)' },
  '.cm-searchMatch.cm-searchMatch-selected': { backgroundColor: 'var(--accent-soft)', outlineColor: 'var(--accent)' },
  '.cm-tooltip': { border: '1px solid var(--line-strong)', borderRadius: '8px', backgroundColor: 'var(--surface-raised)', boxShadow: 'var(--shadow-small)', color: 'var(--text)' },
  '.cm-tooltip-autocomplete > ul': { padding: '4px', minWidth: '140px', fontFamily: 'var(--mono)', fontSize: '13px' },
  '.cm-tooltip-autocomplete > ul > li': { padding: '4px 9px', borderRadius: '5px' },
  '.cm-tooltip-autocomplete > ul > li[aria-selected=true]': { backgroundColor: 'var(--accent-soft)', color: 'var(--accent-strong)' },
  '.cm-completionDetail': { color: 'var(--text-soft)', fontFamily: 'var(--sans)', fontStyle: 'normal' },
  '.cm-snippetField': { backgroundColor: 'var(--accent-soft)' },
})

function reportSelection() {
  if (!view) return
  const position = view.state.selection.main.head
  const line = view.state.doc.lineAt(position)
  emit('selection', { line: line.number, column: position - line.from + 1, offset: position })
}
function scrollRatio() {
  if (!view) return 0
  const element = view.scrollDOM
  const available = element.scrollHeight - element.clientHeight
  return available > 0 ? element.scrollTop / available : 0
}

function applyLinePrefix(prefix: string) {
  if (!view) return
  const { from, to } = view.state.selection.main
  const first = view.state.doc.lineAt(from)
  const last = view.state.doc.lineAt(to > from ? to - 1 : to)
  const selected = view.state.doc.sliceString(first.from, last.to)
  const lines = selected.split('\n')
  const remove = lines.every(line => line.startsWith(prefix))
  const replacement = lines.map(line => remove ? line.slice(prefix.length) : prefix + line).join('\n')
  view.dispatch({ changes: { from: first.from, to: last.to, insert: replacement }, selection: { anchor: first.from + (remove ? 0 : prefix.length), head: first.from + replacement.length } })
  view.focus()
}

function applyMarkup(action: MarkupAction) {
  if (!view || props.readOnly) return
  if (action === 'heading') return applyLinePrefix('## ')
  if (action === 'quote') return applyLinePrefix('> ')
  if (action === 'unordered') return applyLinePrefix('- ')
  if (action === 'ordered') return applyLinePrefix('1. ')
  const { from, to } = view.state.selection.main
  const selected = view.state.doc.sliceString(from, to)
  const snippets: Record<string, { before: string; after: string; fallback: string }> = {
    bold: { before: '**', after: '**', fallback: '加粗文字' },
    italic: { before: '*', after: '*', fallback: '斜体文字' },
    strike: { before: '~~', after: '~~', fallback: '删除线文字' },
    code: { before: '`', after: '`', fallback: '代码' },
    link: { before: '[', after: '](https://)', fallback: '链接文字' },
    codeBlock: { before: '\n```text\n', after: '\n```\n', fallback: '代码内容' },
    inlineMath: { before: '$', after: '$', fallback: 'x^2' },
    displayMath: { before: '\n$$\n', after: '\n$$\n', fallback: 'E = mc^2' },
    callout: { before: '\n::: proof\n', after: '\n:::\n', fallback: '在此输入证明。' },
    table: { before: '\n| 列标题 | 列标题 |\n| --- | --- |\n| ', after: ' | 内容 |\n', fallback: '内容' },
    divider: { before: '\n\n---\n\n', after: '', fallback: '' },
  }
  const snippet = snippets[action]
  if (!snippet) return
  const content = selected || snippet.fallback
  const insert = `${snippet.before}${content}${snippet.after}`
  const anchor = from + snippet.before.length
  view.dispatch({ changes: { from, to, insert }, selection: { anchor, head: anchor + content.length } })
  view.focus()
}

defineExpose({
  applyMarkup,
  captureImageTarget,
  selectSource,
  removeImageReferences,
  scrollRatio,
  focus: () => view?.focus(),
  search: () => { if (view) openSearchPanel(view) },
  complete: () => {
    if (!view) return
    view.focus()
    startCompletion(view)
  },
})

onMounted(() => {
  if (!host.value) return
  view = new EditorView({
    parent: host.value,
    state: EditorState.create({
      doc: props.modelValue,
      extensions: [
        Prec.highest(keymap.of([
          { key: 'Mod-b', run: () => { emit('command', 'bold'); return true } },
          { key: 'Mod-i', run: () => { emit('command', 'italic'); return true } },
          { key: 'Mod-s', run: () => { emit('command', 'save'); return true } },
          { key: 'Enter', run: editor => completionStatus(editor.state) === 'active' && acceptCompletion(editor) },
        ])),
        basicSetup,
        editable.of(EditorState.readOnly.of(!!props.readOnly)),
        EditorView.contentAttributes.of({ 'aria-label': 'Markdown 正文' }),
        EditorView.domEventHandlers({
          paste(event) {
            const files = imageFiles(event.clipboardData?.files)
            if (props.readOnly || !files.length) return false
            const target = captureImageTarget()
            if (target) emit('images', files, target)
            return true
          },
          drop(event, editor) {
            const files = imageFiles(event.dataTransfer?.files)
            if (props.readOnly || !files.length) return false
            const position = editor.posAtCoords({ x: event.clientX, y: event.clientY })
            if (position !== null) editor.dispatch({ selection: { anchor: position } })
            const target = captureImageTarget()
            if (target) emit('images', files, target)
            return true
          },
        }),
        EditorState.tabSize.of(2),
        noteLanguage,
        autocompletion({ interactionDelay: 0 }),
        tooltips({ tooltipSpace: editor => {
          const { left, right, top, bottom } = editor.scrollDOM.getBoundingClientRect()
          const height = window.visualViewport?.height ?? window.innerHeight
          return { left, right, top, bottom: Math.min(bottom, height + (window.visualViewport?.offsetTop ?? 0)) }
        } }),
        EditorState.languageData.of(() => [{ autocomplete: completeMarkdown }]),
        EditorState.phrases.of(editorPhrases),
        EditorView.lineWrapping,
        placeholder(props.placeholder || ''),
        editorTheme,
        syntaxHighlighting(noteHighlight),
        EditorView.updateListener.of(update => {
          if (update.docChanged) {
            for (const target of tracked) {
              const range = target.original !== undefined
              target.from = update.changes.mapPos(target.from, range ? 1 : -1)
              target.to = update.changes.mapPos(target.to, -1)
            }
            emit('update:modelValue', update.state.doc.toString())
          }
          if (update.selectionSet || update.docChanged) reportSelection()
        }),
      ],
    }),
  })
  view.scrollDOM.addEventListener('scroll', () => emit('scroll', scrollRatio()), { passive: true })
  reportSelection()
})

watch(() => props.modelValue, value => {
  if (!view || value === view.state.doc.toString()) return
  view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: value } })
})
watch(() => props.readOnly, value => view?.dispatch({ effects: editable.reconfigure(EditorState.readOnly.of(!!value)) }))

onBeforeUnmount(() => {
  tracked.clear()
  view?.destroy()
  view = undefined
})
</script>

<template>
  <div ref="host" class="markdown-codemirror" :data-placeholder="placeholder" />
</template>
