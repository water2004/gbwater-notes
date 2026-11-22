function fieldValue(line: string) {
  const value = line.trim()
  try {
    const decoded: unknown = JSON.parse(value)
    return typeof decoded === 'string' ? decoded : value
  } catch {
    return value
  }
}

export function splitMarkdown(source: string) {
  const content = source.replace(/^\uFEFF/, '').replace(/\r\n?/g, '\n')
  const match = /^---\n([\s\S]*?)^---[ \t]*(?:\n|$)/m.exec(content)
  if (!match || match.index !== 0) return { fields: {}, lines: [], body: content }
  const lines = match[1].trimEnd().split('\n')
  const fields: Record<string, string> = {}
  for (const line of lines) {
    const field = /^([\w-]+):\s*(.*)$/.exec(line)
    if (!field) continue
    fields[field[1]] = fieldValue(field[2])
  }
  return { fields, lines, body: content.slice(match[0].length).replace(/^\n/, '') }
}

export function composeMarkdown(fields: Record<string, string | number | boolean>, original: string[], body: string) {
  const remaining = new Map(Object.entries(fields))
  const header = original.map(line => {
    const field = /^([\w-]+):(.*)$/.exec(line)
    if (!field || !Object.hasOwn(fields, field[1])) return line
    const [, key, originalValue] = field
    remaining.delete(key)
    return fieldValue(originalValue) === String(fields[key]) ? line : `${key}: ${JSON.stringify(fields[key])}`
  })
  for (const [key, value] of remaining) header.push(`${key}: ${JSON.stringify(value)}`)
  return `---\n${header.join('\n')}\n---\n\n${body}${body.endsWith('\n') ? '' : '\n'}`
}

export function downloadMarkdown(content: string, title: string) {
  const url = URL.createObjectURL(new Blob([content], { type: 'text/markdown;charset=utf-8' }))
  const link = document.createElement('a')
  link.href = url
  link.download = `${title || '笔记'}.md`
  link.click()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}

type Change = { kind: 'added' | 'removed' | 'same'; text: string }

// 先裁掉相同的首尾。对大量同时改写的行限制比较开销，保留明确的整块变更。
export function compareMarkdown(earlier: string, current: string) {
  const lines = (value: string) => value.replace(/\r\n?/g, '\n').split('\n')
  const old = lines(earlier)
  const next = lines(current)
  let start = 0, end = 0
  while (start < Math.min(old.length, next.length) && old[start] === next[start]) start++
  while (start + end < Math.min(old.length, next.length) && old[old.length - end - 1] === next[next.length - end - 1]) end++
  const a = old.slice(start, old.length - end)
  const b = next.slice(start, next.length - end)
  const changes: Change[] = old.slice(Math.max(0, start - 2), start).map(text => ({ kind: 'same', text }))
  if (a.length && b.length && a.length * b.length <= 1_500_000) {
    const cols = b.length + 1
    const table = new Uint32Array((a.length + 1) * cols)
    for (let i = a.length - 1; i >= 0; i--) {
      for (let j = b.length - 1; j >= 0; j--) {
        table[i * cols + j] = a[i] === b[j] ? 1 + table[(i + 1) * cols + j + 1]
          : Math.max(table[(i + 1) * cols + j], table[i * cols + j + 1])
      }
    }
    let i = 0, j = 0
    while (i < a.length || j < b.length) {
      if (i < a.length && j < b.length && a[i] === b[j]) {
        changes.push({ kind: 'same', text: a[i++] }); j++
      } else if (j === b.length || (i < a.length && table[(i + 1) * cols + j] >= table[i * cols + j + 1])) {
        changes.push({ kind: 'removed', text: a[i++] })
      } else {
        changes.push({ kind: 'added', text: b[j++] })
      }
    }
  } else {
    for (const text of a) changes.push({ kind: 'removed', text })
    for (const text of b) changes.push({ kind: 'added', text })
  }
  if (end) for (const text of old.slice(old.length - end, old.length - end + 2)) changes.push({ kind: 'same', text })
  const added = changes.filter(line => line.kind === 'added').length
  const removed = changes.filter(line => line.kind === 'removed').length
  return { added, removed, changes: changes.slice(0, 800), truncated: changes.length > 800 }
}
