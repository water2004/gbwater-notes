import { api, apiUrl } from '@/lib/api'

export interface AdminAsset {
  id: string
  name: string
  modified: string
  bytes: number
}

export interface AssetReference {
  id: string
  title: string
}

const markdownPrefix = '/content-assets/'
export const acceptedImages = '.png,.jpg,.jpeg,.gif,.webp'
const maxImageBytes = 10 * 1024 * 1024

export async function listAssets(trash: boolean, signal: AbortSignal, noteId?: string) {
  const params = new URLSearchParams({ trash: String(trash) })
  if (noteId) params.set('noteId', noteId)
  const data = await api<{ assets: AdminAsset[] }>(`/admin/assets?${params}`, { signal })
  return data.assets
}

export async function uploadAsset(file: File, noteId = '', signal?: AbortSignal) {
  if (file.size > maxImageBytes) throw new Error('单张图片最大 10 MB')
  const data = await api<{ asset: AdminAsset }>(`/admin/assets?${new URLSearchParams({ name: file.name, noteId })}`, {
    method: 'POST',
    file,
    signal,
  })
  return data.asset
}

export function moveAsset(asset: Pick<AdminAsset, 'id'>, restore: boolean) {
  return api('/admin/assets', { method: 'PATCH', json: { id: asset.id, restore } })
}

export async function listAssetReferences(id: string, signal: AbortSignal) {
  const data = await api<{ references: AssetReference[] }>(`/admin/asset/references?${new URLSearchParams({ id })}`, { signal })
  return data.references
}

export function assetPreviewURL(id: string, trash = false) {
  return apiUrl(`/admin/asset/content?${new URLSearchParams({ id, trash: String(trash) })}`)
}

export function imagePath(asset: AdminAsset, base = '/') {
  const path = asset.id.split('/').map(part =>
    encodeURIComponent(part).replace(/[()']/g, char => '%' + char.charCodeAt(0).toString(16)),
  ).join('/')
  return base + 'content-assets/' + path
}

export function escapeImageAlt(alt: string) {
  return alt.replace(/\r?\n/g, ' ').replace(/[\\[\]]/g, '\\$&')
}

export function imageMarkdown(asset: AdminAsset) {
  const alt = escapeImageAlt(asset.name.replace(/\.[^.]+$/, ''))
  return `![${alt}](${imagePath(asset)})`
}

export function editorImageId(source: string, assetBase: string) {
  const prefix = [markdownPrefix, '/mypdf/content-assets/', `${assetBase}content-assets/`].find(prefix => source.startsWith(prefix))
  if (prefix) {
    try {
      return decodeURIComponent(source.slice(prefix.length).split(/[?#]/, 1)[0])
    } catch { /* 保留原始地址，便于定位输入错误 */ }
  }
  return undefined
}

export function editorImageURL(source: string, assetBase: string) {
  const id = editorImageId(source, assetBase)
  return id ? assetPreviewURL(id) : undefined
}
