import { onMounted, ref } from 'vue'
import { api } from '@/lib/api'
import { onUserChange, session } from '@/lib/session'

const STORAGE_KEY = 'gbwater-completed'
const completed = ref<Set<string>>(new Set())
const lastRead = ref<Record<string, string>>({})
let loaded = false

interface ProgressRecord { noteId: string; completed: boolean; lastReadAt?: string; updatedAt: string }

function loadLocal() {
  if (loaded || typeof localStorage === 'undefined') return
  try {
    completed.value = new Set(JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]'))
    lastRead.value = JSON.parse(localStorage.getItem('gbwater-last-read') || '{}')
  } catch (_) {
    completed.value = new Set()
    lastRead.value = {}
  }
  loaded = true
}

function saveLocal() {
  if (typeof localStorage === 'undefined') return
  localStorage.setItem(STORAGE_KEY, JSON.stringify([...completed.value]))
  localStorage.setItem('gbwater-last-read', JSON.stringify(lastRead.value))
}

function localRecords(): ProgressRecord[] {
  const ids = new Set([...completed.value, ...Object.keys(lastRead.value)])
  return [...ids].map(noteId => ({
    noteId,
    completed: completed.value.has(noteId),
    lastReadAt: lastRead.value[noteId],
    updatedAt: lastRead.value[noteId] || new Date(0).toISOString(),
  }))
}

function applyServer(records: ProgressRecord[]) {
  completed.value = new Set(records.filter(record => record.completed).map(record => record.noteId))
  lastRead.value = Object.fromEntries(records.filter(record => record.lastReadAt).map(record => [record.noteId, record.lastReadAt as string]))
  saveLocal()
}

async function pullServer() {
  const data = await api<{ progress: ProgressRecord[]; favorites: { noteId: string; createdAt: string }[] }>('/me/reading')
  applyServer(data.progress)
}

// adoptOwner 在同一浏览器换账号时清掉上一位用户的本地记录，
// 避免把别人的阅读进度合并进新账号。
function adoptOwner(userId: string) {
  if (typeof localStorage === 'undefined') return
  const owner = localStorage.getItem('gbwater-local-owner')
  if (owner && owner !== userId) {
    localStorage.removeItem(STORAGE_KEY)
    localStorage.removeItem('gbwater-last-read')
    completed.value = new Set()
    lastRead.value = {}
    loaded = true
  }
  localStorage.setItem('gbwater-local-owner', userId)
}

// mergeLocal 把未登录期间的阅读记录并入云端，再以云端结果为准。
async function mergeLocal() {
  if (!session.user) return
  adoptOwner(session.user.id)
  const records = localRecords()
  if (records.length) {
    await api('/me/reading/merge', { method: 'POST', json: { progress: records } })
  }
  await pullServer()
}

async function syncProgress(noteId: string, payload: { completed?: boolean; read?: boolean }) {
  if (!session.user) return
  try {
    await api('/me/reading', { method: 'PUT', json: { noteId, ...payload } })
  } catch (_) {
    /* 离线时保留本地状态，下次登录合并。 */
  }
}

export function useReadingProgress() {
  onMounted(loadLocal)

  async function toggleCompleted(id: string) {
    loadLocal()
    const next = new Set(completed.value)
    const value = !next.has(id)
    if (value) next.add(id)
    else next.delete(id)
    completed.value = next
    saveLocal()
    await syncProgress(id, { completed: value })
  }

  function markRead(id: string) {
    loadLocal()
    lastRead.value = { ...lastRead.value, [id]: new Date().toISOString() }
    saveLocal()
    void syncProgress(id, { read: true })
  }

  return { completed, lastRead, toggleCompleted, markRead }
}

onUserChange(user => {
  if (typeof window === 'undefined') return
  if (user) {
    void mergeLocal().catch(() => undefined)
  } else {
    loadLocal()
  }
})
