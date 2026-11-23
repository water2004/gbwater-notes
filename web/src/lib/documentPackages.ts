import { api, apiZip } from './api'
import type { AdminNote } from './adminNotes'

export const maxDocumentPackageSize = 64 * 1024 * 1024

export interface DocumentPackagePlan {
  digest: string
  notes: { note: AdminNote; action: 'create' | 'reuse' | 'replace' | 'conflict' }[]
  newNotes: number
  replaceNotes: number
  newAssets: number
  reusedAssets: number
  remappedAssets: string[]
  recycledAssets: string[]
  categories: string[]
  conflicts: { kind: 'note' | 'asset'; id: string; reason: string }[]
}

export async function downloadDocumentPackage(ids: string[], filename: string) {
  const zip = await apiZip('/admin/document-packages/export', { method: 'POST', json: { ids } })
  const url = URL.createObjectURL(zip)
  const link = document.createElement('a')
  link.href = url
  link.download = `${filename}.gbnotes.zip`
  link.click()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}

export function previewDocumentPackage(file: File, signal: AbortSignal) {
  return api<DocumentPackagePlan>('/admin/document-packages?preview=true', { method: 'POST', file, signal })
}

export function importDocumentPackage(file: File, digest: string, replace: boolean) {
  const query = new URLSearchParams({ digest, replace: String(replace) })
  return api<DocumentPackagePlan>(`/admin/document-packages?${query}`, { method: 'POST', file })
}
