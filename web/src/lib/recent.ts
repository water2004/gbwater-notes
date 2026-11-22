import { ref } from 'vue'
import { publishedNotes } from '@/lib/content'

const recentIds = ref<string[]>([])
let loaded = false

function load() {
  if (loaded || typeof localStorage === 'undefined') return
  try { recentIds.value = JSON.parse(localStorage.getItem('gbwater-recent-notes') || '[]') } catch (_) { recentIds.value = [] }
  loaded = true
}

export function useRecentNotes() {
  function markRecent(id: string) {
    load()
    recentIds.value = [id, ...recentIds.value.filter(item => item !== id)].slice(0, 6)
    localStorage.setItem('gbwater-recent-notes', JSON.stringify(recentIds.value))
  }
  function clearRecent() {
    recentIds.value = []
    localStorage.removeItem('gbwater-recent-notes')
  }
  const notes = () => recentIds.value.map(id => publishedNotes.find(note => note.id === id)).filter(note => note !== undefined)
  return { recentIds, notes, markRecent, clearRecent, loadRecent: load }
}
