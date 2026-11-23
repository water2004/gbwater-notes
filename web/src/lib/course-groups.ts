import type { NoteMeta } from '../types/content'

export function groupCourses(notes: NoteMeta[], category?: string) {
  const groups = new Map<string, { category: string; course: string; courseSlug: string; notes: NoteMeta[] }>()
  for (const note of notes) {
    if (category && note.category !== category) continue
    const key = `${note.category}/${note.course}`
    const current = groups.get(key) || { category: note.category, course: note.course, courseSlug: note.courseSlug, notes: [] }
    current.notes.push(note)
    groups.set(key, current)
  }
  return [...groups.values()].map(group => ({
    ...group,
    notes: group.notes.sort((a, b) => a.order - b.order || a.title.localeCompare(b.title, 'zh-CN')),
  }))
}
