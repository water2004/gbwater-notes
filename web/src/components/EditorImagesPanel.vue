<script setup lang="ts">
import { siteBase } from '@/lib/paths'
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import AppIcon from './AppIcon.vue'
import {
  assetPreviewURL, editorImageId, editorImageURL, listAssetReferences, listAssets, moveAsset,
  type AdminAsset, type AssetReference,
} from '@/lib/adminAssets'
import { adminEditorPath } from '@/lib/adminNotes'
import type { ImageReference } from '@/lib/editorImages'

const props = defineProps<{
  references: ImageReference[]
  noteId: string
  active: number | undefined
  tab: 'used' | 'browse' | 'trash'
  busy: boolean
  refresh: number
  queued: string[]
  recycledHere: string[]
  unlinkedHere: string[]
}>()
const emit = defineEmits<{
  close: []
  tab: [tab: 'used' | 'browse' | 'trash']
  activate: [index: number | undefined]
  trash: [id: string, savedHere: boolean]
  cancelTrash: [id: string]
  restored: [id: string]
  locate: [reference: ImageReference]
  changeAlt: [reference: ImageReference, alt: string]
  replace: [reference: ImageReference]
  remove: [reference: ImageReference]
  insert: [asset: AdminAsset]
  upload: []
}>()
const items = ref<AdminAsset[]>()
const owned = ref<AdminAsset[]>([])
const recycled = ref<AdminAsset[]>([])
const inventoryError = ref('')
const inventoryLoading = ref(false)
const selectedFile = ref<string>()
const usage = ref<AssetReference[]>()
const usageError = ref('')
const confirmTrash = ref(false)
const restoring = ref('')
const search = ref('')
const scope = ref<'near' | 'all'>('near')
const visibleCount = ref(18)
const error = ref('')
const panelClose = ref<HTMLButtonElement>()
const detail = ref<HTMLElement>()
const alt = ref('')
const requests = new AbortController()
const base = siteBase
const selected = computed(() => props.active === undefined ? undefined : props.references[props.active])
const selectedId = computed(() => selected.value ? editorImageId(selected.value.url, base) : selectedFile.value)
const usedIds = computed(() => new Set(props.references.map(image => editorImageId(image.url, base))))
const unused = computed(() => [...new Set([...owned.value.map(asset => asset.id), ...props.unlinkedHere])]
  .filter(id => !usedIds.value.has(id) && !props.queued.includes(id) && !props.recycledHere.includes(id)))
const recycledIds = computed(() => [...new Set([...recycled.value.map(asset => asset.id), ...props.recycledHere])])
const elsewhere = computed(() => usage.value?.filter(note => note.id !== props.noteId) ?? [])
const savedHere = computed(() => usage.value?.some(note => note.id === props.noteId) ?? false)
const localUseCount = computed(() => props.references.filter(image => editorImageId(image.url, base) === selectedId.value).length)
const isQueued = computed(() => !!selectedId.value && props.queued.includes(selectedId.value))
let inventoryRequest: AbortController | undefined
let usageRequest: AbortController | undefined
const matches = computed(() => {
  const query = search.value.trim().toLocaleLowerCase()
  return (items.value ?? []).filter(asset => {
    const nearby = asset.id.startsWith('uploads/') || (!!props.noteId && asset.id.startsWith(props.noteId + '/'))
    return (query || scope.value === 'all' || nearby) && asset.id.toLocaleLowerCase().includes(query)
  }).sort((a, b) => Number(b.id.startsWith(props.noteId + '/')) - Number(a.id.startsWith(props.noteId + '/')))
})
watch(selected, (reference, previous) => {
  if (reference?.alt !== previous?.alt || reference?.url !== previous?.url) alt.value = reference?.alt ?? ''
}, { immediate: true })
watch([search, scope], () => { visibleCount.value = 18 })

async function load() {
  error.value = ''
  try {
    items.value = await listAssets(false, requests.signal)
  } catch (reason) {
    if (!requests.signal.aborted) error.value = reason instanceof Error ? reason.message : '读取图床失败'
  }
}
watch(() => props.tab, tab => {
  if (tab === 'browse' && !items.value) void load()
}, { immediate: true })
watch(panelClose, button => button?.focus(), { once: true })
async function loadInventory() {
  if (!props.noteId || props.tab === 'browse') return
  inventoryRequest?.abort()
  const request = new AbortController()
  inventoryRequest = request
  inventoryLoading.value = true
  inventoryError.value = ''
  const trash = props.tab === 'trash'
  try {
    const assets = await listAssets(trash, request.signal, props.noteId)
    if (!request.signal.aborted) {
      if (trash) recycled.value = assets
      else owned.value = assets
    }
  } catch (reason) {
    if (!request.signal.aborted) inventoryError.value = reason instanceof Error ? reason.message : '读取本篇图片失败'
  } finally {
    if (!request.signal.aborted) inventoryLoading.value = false
  }
}
async function loadUsage() {
  confirmTrash.value = false
  usageRequest?.abort()
  usage.value = undefined
  usageError.value = ''
  if (!selectedId.value || props.tab !== 'used') return
  const request = new AbortController()
  usageRequest = request
  try {
    const result = await listAssetReferences(selectedId.value, request.signal)
    if (!request.signal.aborted) usage.value = result
  } catch (reason) {
    if (!request.signal.aborted) usageError.value = reason instanceof Error ? reason.message : '读取图片引用失败'
  }
}
watch([() => props.tab, () => props.refresh], loadInventory, { immediate: true })
watch([selectedId, () => props.refresh, () => props.tab], loadUsage, { immediate: true })
watch(() => props.active, index => { if (index !== undefined) selectedFile.value = undefined })
watch(() => props.refresh, () => {
  if (items.value) void load()
})
watch(() => props.recycledHere, ids => {
  if (selectedFile.value && ids.includes(selectedFile.value)) selectedFile.value = undefined
})
onBeforeUnmount(() => {
  requests.abort()
  inventoryRequest?.abort()
  usageRequest?.abort()
})

async function selectFile(id: string) {
  emit('activate', undefined)
  selectedFile.value = id
  await revealDetail()
}

async function selectReference(index: number) {
  selectedFile.value = undefined
  emit('activate', index)
  await revealDetail()
}

async function revealDetail() {
  await nextTick()
  detail.value?.scrollIntoView({ block: 'nearest', inline: 'nearest' })
}

async function restore(id: string) {
  if (restoring.value) return
  restoring.value = id
  inventoryError.value = ''
  try {
    await moveAsset({ id }, true)
    recycled.value = recycled.value.filter(item => item.id !== id)
    emit('restored', id)
  } catch (reason) {
    inventoryError.value = reason instanceof Error ? reason.message : '恢复图片失败'
  } finally {
    restoring.value = ''
  }
}

function trashSelected() {
  if (!selectedId.value || !usage.value || elsewhere.value.length) return
  const id = selectedId.value
  confirmTrash.value = false
  emit('trash', id, savedHere.value)
}

function fileName(id: string) {
  return id.split('/').at(-1) ?? id
}
function preview(reference: ImageReference) {
  return editorImageURL(reference.url, base) ?? (/^https?:\/\//i.test(reference.url) ? reference.url : undefined)
}

function label(reference: ImageReference) {
  if (reference.alt) return reference.alt
  const path = reference.url.split(/[?#]/, 1)[0].split('/').at(-1) || '图片'
  try { return decodeURIComponent(path) } catch { return path }
}
</script>

<template>
  <aside class="editor-images-panel" aria-label="编辑器图片">
    <header>
      <h2>图片管理</h2>
      <button ref="panelClose" type="button" class="icon-button" aria-label="关闭图片面板" @click="emit('close')"><AppIcon name="close" :size="17" /></button>
    </header>
    <div class="segmented-control editor-images-tabs" role="group" aria-label="图片视图">
      <button type="button" :aria-pressed="tab === 'used'" @click="emit('tab', 'used')">本篇</button>
      <button type="button" :aria-pressed="tab === 'browse'" @click="emit('tab', 'browse')">图库</button>
      <button v-if="noteId || recycledHere.length || tab === 'trash'" type="button" :aria-pressed="tab === 'trash'" @click="emit('tab', 'trash')">回收站</button>
    </div>
    <div v-if="tab === 'used'" class="editor-images-content">
      <div v-for="id in queued" :key="id" class="note-image-queue" role="status">
        <span><strong>{{ fileName(id) }}</strong><small>保存笔记后回收</small></span>
        <button type="button" class="text-link" @click="emit('cancelTrash', id)">取消回收</button>
      </div>

      <section v-if="selected || selectedFile" ref="detail" class="note-image-detail" aria-label="选中图片">
        <div class="note-image-selected">
          <img v-if="selected ? preview(selected) : selectedFile" :src="selected ? preview(selected) : assetPreviewURL(selectedFile!)" alt="">
          <AppIcon v-else name="image" :size="24" />
          <div><strong>{{ selected ? label(selected) : fileName(selectedFile!) }}</strong>
            <small v-if="selected">{{ selectedId ? fileName(selectedId) : '外部图片' }}</small>
          </div>
        </div>
        <form v-if="selected" class="note-image-alt" @submit.prevent="emit('changeAlt', selected, alt)">
          <label class="form-field"><span>图片说明</span><input v-model="alt" :disabled="busy" placeholder="简要描述图像内容"></label>
          <button class="button button-quiet button-compact" type="submit" :disabled="busy || alt === selected.alt">应用</button>
        </form>
        <div v-if="selected" class="note-image-actions">
          <button type="button" class="button button-quiet button-compact" @click="emit('locate', selected)">定位</button>
          <button type="button" class="button button-quiet button-compact" :disabled="busy" @click="emit('replace', selected)">替换</button>
          <button type="button" class="button button-quiet button-compact" :disabled="busy" @click="emit('remove', selected)">移出正文</button>
        </div>
        <div v-if="selectedId" class="note-image-storage">
          <p v-if="usageError" class="form-hint">{{ usageError }} <button type="button" class="text-link" @click="loadUsage">重试</button></p>
          <p v-else-if="!usage" class="form-hint">正在检查引用…</p>
          <template v-else-if="elsewhere.length">
            <p class="form-hint">其他笔记在用</p>
            <RouterLink v-for="note in elsewhere" :key="note.id" class="text-link" :to="adminEditorPath(note.id)" target="_blank">{{ note.title }}</RouterLink>
          </template>
          <div v-else-if="confirmTrash" class="note-image-confirm">
            <p>{{ savedHere ? '移除本文中的全部引用，保存后回收原图。' : localUseCount ? '移除本文中的引用，并回收原图。' : '将原图移到回收站，可随时恢复。' }}</p>
            <div class="note-image-actions">
              <button type="button" class="button button-quiet button-compact" @click="confirmTrash = false">取消</button>
              <button type="button" class="button button-danger button-compact" :disabled="busy" @click="trashSelected">{{ savedHere ? '移除并排队回收' : '确认回收' }}</button>
            </div>
          </div>
          <button v-else-if="!isQueued" type="button" class="button button-danger button-compact" :disabled="busy" @click="confirmTrash = true">移到回收站</button>
        </div>
      </section>

      <p v-if="inventoryError" class="error-message">{{ inventoryError }} <button type="button" class="text-link" @click="loadInventory">重试</button></p>
      <section v-if="references.length" class="note-image-group">
        <h3>正文引用 <span>{{ references.length }}</span></h3>
        <div class="note-image-grid">
          <button v-for="(image, index) in references" :key="index" type="button" :aria-pressed="active === index && !selectedFile"
            :aria-label="`编辑第 ${index + 1} 张图片 ${label(image)}`" @click="selectReference(index)">
            <img v-if="preview(image)" :src="preview(image)" alt="" loading="lazy"><AppIcon v-else name="image" :size="21" />
            <span :title="label(image)">{{ label(image) }}</span>
          </button>
        </div>
      </section>
      <section v-if="unused.length" class="note-image-group">
        <h3>未插入本篇 <span>{{ unused.length }}</span></h3>
        <div class="note-image-grid">
          <button v-for="id in unused" :key="id" type="button" :aria-pressed="selectedFile === id" :aria-label="`管理 ${fileName(id)}`" @click="selectFile(id)">
            <img :src="assetPreviewURL(id)" alt="" loading="lazy"><span :title="fileName(id)">{{ fileName(id) }}</span>
          </button>
        </div>
      </section>
      <div v-if="!references.length && !unused.length && !inventoryLoading" class="editor-images-empty">
        <AppIcon name="image" :size="25" /><p>本篇还没有图片</p>
        <button type="button" class="button button-quiet button-compact" :disabled="busy" @click="emit('upload')">上传图片</button>
      </div>
    </div>
    <div v-else-if="tab === 'trash'" class="editor-images-content">
      <p v-if="inventoryError" class="error-message">{{ inventoryError }} <button type="button" class="text-link" @click="loadInventory">重试读取</button></p>
      <p v-if="inventoryLoading" class="form-hint">正在读取回收站…</p>
      <p v-else-if="!recycledIds.length" class="form-hint">本篇的回收站为空。</p>
      <div v-for="id in recycledIds" :key="id" class="note-image-recycled">
        <img :src="assetPreviewURL(id, true)" alt="" loading="lazy"><strong :title="id">{{ fileName(id) }}</strong>
        <button type="button" class="button button-quiet button-compact" :disabled="!!restoring || busy" :aria-label="`恢复 ${fileName(id)}`" @click="restore(id)">恢复</button>
      </div>
    </div>
    <div v-else class="editor-images-content">
      <label class="admin-filter-search"><AppIcon name="search" :size="16" /><input v-model="search" type="search" aria-label="在图床中搜索" placeholder="搜索图片名或路径"></label>
      <div v-if="!search.trim()" class="segmented-control editor-images-scope" role="group" aria-label="图库范围">
        <button type="button" :aria-pressed="scope === 'near'" @click="scope = 'near'">本篇与共用</button>
        <button type="button" :aria-pressed="scope === 'all'" @click="scope = 'all'">全部图片</button>
      </div>
      <p v-if="error" class="error-message">{{ error }} <button type="button" class="text-link" @click="load">重试</button></p>
      <p v-else-if="!items" class="form-hint">正在读取图床…</p>
      <div v-else-if="matches.length" class="note-image-grid">
        <button v-for="asset in matches.slice(0, visibleCount)" :key="asset.id" type="button" :disabled="busy"
          :aria-label="`插入 ${asset.name}`" :title="asset.id" @click="emit('insert', asset)">
          <img :src="assetPreviewURL(asset.id)" alt="" loading="lazy"><span>{{ asset.name }}</span>
        </button>
      </div>
      <p v-else class="form-hint">{{ search ? '没有匹配的图片' : '此范围暂无图片。' }}</p>
      <button v-if="visibleCount < matches.length" type="button" class="button button-quiet button-compact" @click="visibleCount += 18">显示更多</button>
    </div>
  </aside>
</template>
