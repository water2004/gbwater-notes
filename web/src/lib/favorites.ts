import { onMounted, ref } from 'vue'
import { api } from '@/lib/api'
import { onUserChange, session } from '@/lib/session'

const STORAGE_KEY = 'gbwater-favorites'
const favorites = ref<Set<string>>(new Set())
let loaded = false

function loadLocal() {
  if (loaded || typeof localStorage === 'undefined') return
  try {
    favorites.value = new Set(JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]'))
  } catch (_) {
    favorites.value = new Set()
  }
  loaded = true
}

function saveLocal() {
  if (typeof localStorage === 'undefined') return
  localStorage.setItem(STORAGE_KEY, JSON.stringify([...favorites.value]))
}

async function pullServer() {
  const data = await api<{ favorites: { noteId: string }[] }>('/api/me/reading')
  favorites.value = new Set(data.favorites.map(item => item.noteId))
  saveLocal()
}

async function mergeLocal() {
  if (!session.user) return
  const owner = localStorage.getItem('gbwater-local-owner-favorites')
  if (owner && owner !== session.user.id) {
    localStorage.removeItem(STORAGE_KEY)
    favorites.value = new Set()
    loaded = true
  }
  localStorage.setItem('gbwater-local-owner-favorites', session.user.id)
  if (favorites.value.size) {
    await api('/api/me/favorites/merge', { method: 'POST', json: { noteIds: [...favorites.value] } })
  }
  await pullServer()
}

export function useFavorites() {
  onMounted(loadLocal)

  async function toggleFavorite(noteId: string) {
    loadLocal()
    const next = new Set(favorites.value)
    const added = !next.has(noteId)
    if (added) next.add(noteId)
    else next.delete(noteId)
    favorites.value = next
    saveLocal()
    if (!session.user) return added
    try {
      if (added) {
        await api('/api/me/favorites', { method: 'PUT', json: { noteId } })
      } else {
        await api(`/api/me/favorites?noteId=${encodeURIComponent(noteId)}`, { method: 'DELETE' })
      }
    } catch (_) {
      /* 同步失败不阻塞阅读，下次登录会重新合并。 */
    }
    return added
  }

  function isFavorite(noteId: string) {
    return favorites.value.has(noteId)
  }

  return { favorites, isFavorite, toggleFavorite }
}

onUserChange(user => {
  if (typeof window === 'undefined') return
  if (user) void mergeLocal().catch(() => undefined)
  else loadLocal()
})
