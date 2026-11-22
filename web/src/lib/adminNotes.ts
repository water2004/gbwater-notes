import { api, apiUrl } from '@/lib/api'
import type { NativeDocument } from '@/types/content'

export interface AdminNote {
  id: string
  title: string
  category: string
  categoryTitle: string
  course: string
  slug: string
  courseSlug: string
  order: number
  draft: boolean
  revision: string
  modified: string
  bytes: number
}

interface AdminDocument {
  note: AdminNote
  content: string
}

export interface RevisionRecord {
  id: number
  noteId: string
  revision: string
  title: string
  action: 'create' | 'update' | 'baseline'
  createdAt: string
}

type NewNote = Pick<AdminNote, 'title' | 'category' | 'course' | 'slug' | 'courseSlug' | 'order' | 'draft'>

export async function listAdminNotes() {
  const data = await api<{ notes: AdminNote[] }>('/admin/notes')
  return data.notes
}

export function readAdminNote(id: string) {
  return api<AdminDocument>(`/admin/note?${new URLSearchParams({ id })}`)
}

export function readNotePreview(id: string) {
  return api<NativeDocument>(`/admin/note/native?${new URLSearchParams({ id })}`)
}

export function createAdminNote(note: NewNote, body: string) {
  return api<AdminDocument>('/admin/notes', { method: 'POST', json: { note, body } })
}

export function updateAdminNote(id: string, content: string, expectedRevision: string) {
  return api<AdminDocument>('/admin/note', { method: 'PUT', json: { id, content, expectedRevision } })
}

export async function setAdminNoteDraft(note: AdminNote, draft: boolean) {
  const result = await api<{ note: AdminNote }>('/admin/note', {
    method: 'PATCH',
    json: { id: note.id, expectedRevision: note.revision, draft },
  })
  return result.note
}

export function noteDownloadURL(id: string) {
  return apiUrl(`/admin/note/download?${new URLSearchParams({ id })}`)
}

export function adminEditorPath(id: string) {
  return `/admin/editor/${id.split('/').map(encodeURIComponent).join('/')}`
}

export async function listNoteRevisions(noteId: string, signal: AbortSignal) {
  const data = await api<{ revisions: RevisionRecord[] }>(`/admin/revisions?${new URLSearchParams({ noteId })}`, { signal })
  return data.revisions
}

export async function readNoteRevision(noteId: string, revisionId: number, signal: AbortSignal) {
  const data = await api<{ content: string }>(`/admin/revisions/${revisionId}?${new URLSearchParams({ noteId })}`, { signal })
  return data.content
}
