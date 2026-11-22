import { api } from '@/lib/api'

export interface PopularNote {
  id: string
  slug: string
  title: string
  category: string
  categoryTitle: string
  course: string
  courseSlug: string
  views: number
  recentViews: number
}

const attempted = new Set<string>()

export async function getPopularNotes(limit = 6) {
  const response = await api<{ notes: PopularNote[] }>(`/api/public/popular-notes?limit=${limit}`, { cache: 'no-cache' })
  return response.notes
}

export function trackNoteView(noteId: string) {
  if (!noteId || attempted.has(noteId)) return
  const key = `gbwater-note-view:${noteId}`
  try {
    if (sessionStorage.getItem(key)) return
  } catch (_) { /* A memory guard still prevents duplicate mounts. */ }
  attempted.add(noteId)
  try { sessionStorage.setItem(key, '1') } catch (_) { /* Storage can be unavailable in private contexts. */ }
  const body = JSON.stringify({ id: noteId })
  if (navigator.sendBeacon) {
    const accepted = navigator.sendBeacon('/api/public/note-view', new Blob([body], { type: 'application/json' }))
    if (accepted) return
  }
  fetch('/api/public/note-view', {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body,
    credentials: 'same-origin', keepalive: true,
  }).then(response => {
    if (!response.ok) throw new Error(`Note view failed: ${response.status}`)
  }).catch(() => {
    attempted.delete(noteId)
    try { sessionStorage.removeItem(key) } catch (_) { /* Nothing else to clean up. */ }
  })
}
