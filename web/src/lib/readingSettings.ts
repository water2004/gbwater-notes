import { reactive, watch } from 'vue'
import { api } from '@/lib/api'
import { session } from '@/lib/session'

export interface ReadingSettings {
  fontScale: number
  width: number
  lineHeight: number
  font: 'serif' | 'sans'
}

const DEFAULTS: ReadingSettings = { fontScale: 1, width: 760, lineHeight: 2, font: 'serif' }
const STORAGE_KEY = 'gbwater-reading-settings'

function readLocal(): ReadingSettings {
  if (typeof localStorage === 'undefined') return { ...DEFAULTS }
  try {
    const saved = JSON.parse(localStorage.getItem(STORAGE_KEY) || '{}')
    return {
      fontScale: clamp(Number(saved.fontScale) || DEFAULTS.fontScale, 0.85, 1.35),
      width: clamp(Number(saved.width) || DEFAULTS.width, 560, 980),
      lineHeight: clamp(Number(saved.lineHeight) || DEFAULTS.lineHeight, 1.5, 2.4),
      font: saved.font === 'sans' ? 'sans' : 'serif',
    }
  } catch (_) {
    return { ...DEFAULTS }
  }
}

function clamp(value: number, min: number, max: number) {
  return Math.min(Math.max(value, min), Math.max(min, max))
}

export const readingSettings = reactive<ReadingSettings>(readLocal())

let saveTimer: ReturnType<typeof setTimeout> | undefined
let localRevision = 0

function apply(settings: Partial<ReadingSettings>) {
  if (typeof settings.fontScale === 'number') readingSettings.fontScale = clamp(settings.fontScale, 0.85, 1.35)
  if (typeof settings.width === 'number') readingSettings.width = clamp(settings.width, 560, 980)
  if (typeof settings.lineHeight === 'number') readingSettings.lineHeight = clamp(settings.lineHeight, 1.5, 2.4)
  if (settings.font === 'serif' || settings.font === 'sans') readingSettings.font = settings.font
}

function persist() {
  clearTimeout(saveTimer)
  const userID = session.user?.id
  if (!userID) return
  const payload = { reading: { ...readingSettings } }
  saveTimer = setTimeout(() => {
    if (session.user?.id !== userID) return
    void api('/me/settings', {
      method: 'PUT',
      json: payload,
    }).catch(() => { /* 离线时保留本地设置 */ })
  }, 500)
}

// 登录后拉取账号设置；没有保存过就沿用本地。
async function syncReadingSettings(userID: string, signal: AbortSignal) {
  const revision = localRevision
  try {
    const data = await api<{ reading?: Partial<ReadingSettings> }>('/me/settings', { signal })
    if (signal.aborted || session.user?.id !== userID || revision !== localRevision) return
    if (data.reading && Object.keys(data.reading).length) {
      apply(data.reading)
    } else {
      persist()
    }
  } catch (_) { /* 保留本地设置 */ }
}

export function updateReadingSettings(patch: Partial<ReadingSettings>) {
  localRevision++
  apply(patch)
  persist()
}

export function resetReadingSettings() {
  updateReadingSettings(DEFAULTS)
}

watch(readingSettings, () => {
  try { localStorage.setItem(STORAGE_KEY, JSON.stringify(readingSettings)) } catch (_) { /* 浏览器可能禁用存储 */ }
}, { deep: true })
// 会话恢复是异步的：登录用户一旦出现就拉取账号设置；换账号时重新同步。
watch(() => session.user?.id, (id, _previous, onCleanup) => {
  clearTimeout(saveTimer)
  if (!id) return
  const controller = new AbortController()
  onCleanup(() => controller.abort())
  void syncReadingSettings(id, controller.signal)
}, { immediate: true })
