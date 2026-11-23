<script setup lang="ts">
import { siteBase } from '@/lib/paths'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import AppIcon from './AppIcon.vue'
import { acceptedImages, assetPreviewURL, imageMarkdown, imagePath, listAssetReferences, listAssets, moveAsset, uploadAsset, type AdminAsset, type AssetReference } from '@/lib/adminAssets'
import { adminEditorPath } from '@/lib/adminNotes'

const items = ref<AdminAsset[]>([])
const trash = ref(false)
const folder = ref('')
const selected = ref<AdminAsset>()
const usage = ref<AssetReference[]>()
const usageError = ref('')
const folders = computed(() => [...new Set(items.value.map(item => directory(item.id)))].sort((a, b) => a.localeCompare(b, 'zh-CN')))
const query = ref('')
const page = ref(1)
const pageSize = 24
const loading = ref(false)
const uploading = ref('')
const moving = ref('')
const notice = ref('')
const error = ref('')
const input = ref<HTMLInputElement>()
let request: AbortController | undefined
let usageRequest: AbortController | undefined

const matches = computed(() => {
  const needle = query.value.trim().toLocaleLowerCase()
  return items.value.filter(item => (!folder.value || directory(item.id) === folder.value)
    && (!needle || item.id.toLocaleLowerCase().includes(needle)))
})
const pageCount = computed(() => Math.max(1, Math.ceil(matches.value.length / pageSize)))
const visible = computed(() => matches.value.slice((page.value - 1) * pageSize, page.value * pageSize))
watch([query, folder], () => { page.value = 1 })
watch(pageCount, count => { page.value = Math.min(page.value, count) })

function directory(id: string) {
  const slash = id.lastIndexOf('/')
  return slash < 0 ? '.' : id.slice(0, slash)
}
function folderLabel(value: string) {
  return value === 'uploads' ? '共用图片' : value === '.' ? '根目录' : value
}

function inspect(asset?: AdminAsset) {
  usageRequest?.abort()
  selected.value = asset
  usage.value = undefined
  usageError.value = ''
  if (!asset) return
  const request = new AbortController()
  usageRequest = request
  listAssetReferences(asset.id, request.signal).then(items => {
    if (!request.signal.aborted) usage.value = items
  }).catch(reason => {
    if (!request.signal.aborted) usageError.value = describeError(reason)
  })
}

function describeError(reason: unknown) {
  return reason instanceof Error ? reason.message : '操作失败，请重试'
}

async function load() {
  request?.abort()
  const controller = new AbortController()
  request = controller
  loading.value = true
  try {
    const assets = await listAssets(trash.value, controller.signal)
    if (!controller.signal.aborted) items.value = assets
  } catch (reason) {
    if (!controller.signal.aborted) error.value = describeError(reason)
  } finally {
    if (!controller.signal.aborted) loading.value = false
  }
}

function switchTrash(value: boolean) {
  if (trash.value === value) return
  trash.value = value
  folder.value = ''
  query.value = ''
  inspect()
  error.value = ''
  notice.value = ''
  page.value = 1
  void load()
}

async function upload(files: FileList | null) {
  if (!files?.length || uploading.value || trash.value) return
  error.value = ''
  notice.value = ''
  const queue = Array.from(files)
  const note = folder.value.split('/').length === 3 ? folder.value : undefined
  const failures: string[] = []
  let added = 0
  let latest: AdminAsset | undefined
  for (const [index, file] of queue.entries()) {
    uploading.value = `正在上传 ${index + 1}/${queue.length}`
    try {
      latest = await uploadAsset(file, note)
      added++
    } catch (reason) {
      failures.push(`${file.name}：${describeError(reason)}`)
    }
  }
  uploading.value = ''
  if (input.value) input.value.value = ''
  if (added) {
    notice.value = `已上传 ${added} 张图片`
    page.value = 1
    await load()
    if (latest) {
      folder.value = folder.value && directory(latest.id) !== folder.value ? '' : folder.value
      query.value = ''
      inspect(latest)
    }
  }
  error.value = failures.join('；')
}

async function changeLocation(asset: AdminAsset) {
  if (moving.value) return
  moving.value = asset.id
  error.value = ''
  notice.value = ''
  try {
    await moveAsset(asset, trash.value)
    items.value = items.value.filter(item => item.id !== asset.id)
    if (selected.value?.id === asset.id) inspect()
    notice.value = trash.value ? '已恢复到原位置' : '已移到回收站'
  } catch (reason) {
    error.value = describeError(reason)
  } finally {
    moving.value = ''
  }
}

async function copy(asset: AdminAsset, markdown: boolean) {
  error.value = ''
  try {
    const value = markdown ? imageMarkdown(asset) : new URL(imagePath(asset, siteBase), window.location.href).href
    await navigator.clipboard.writeText(value)
    notice.value = markdown ? '已复制 Markdown' : '已复制公开链接'
  } catch (reason) {
    error.value = describeError(reason)
  }
}

onMounted(load)
onBeforeUnmount(() => {
  request?.abort()
  usageRequest?.abort()
})
</script>

<template>
  <section class="asset-library" aria-label="图片库">
    <div class="asset-toolbar">
      <div class="segmented-control" role="group" aria-label="图床视图">
        <button type="button" :aria-pressed="!trash" :disabled="!!moving || !!uploading" @click="switchTrash(false)">图库</button>
        <button type="button" :aria-pressed="trash" :disabled="!!moving || !!uploading" @click="switchTrash(true)">回收站</button>
      </div>
      <select v-model="folder" aria-label="按目录筛选" :disabled="!!uploading">
        <option value="">全部目录</option>
        <option v-for="dir in folders" :key="dir" :value="dir">{{ folderLabel(dir) }}</option>
      </select>
      <label class="admin-filter-search"><AppIcon name="search" :size="17" /><input v-model="query" type="search" aria-label="搜索图片" placeholder="搜索图片名或路径"></label>
      <button v-if="!trash" class="button button-primary" type="button" :disabled="!!uploading" @click="input?.click()"><AppIcon name="upload" :size="16" />{{ uploading || '批量上传' }}</button>
      <input ref="input" class="visually-hidden" type="file" multiple :accept="acceptedImages" aria-label="选择图片文件" tabindex="-1" @change="upload(($event.target as HTMLInputElement).files)">
    </div>
    <p v-if="error" class="error-message" role="alert">{{ error }}</p>
    <p v-if="notice" class="form-hint" role="status">{{ notice }}</p>

    <div :class="['asset-library-layout', { inspecting: selected }]">
      <div class="asset-gallery">
        <div class="asset-gallery-header" @dragover.prevent @drop.prevent="upload($event.dataTransfer?.files ?? null)">
          <span>{{ matches.length }} 张图片</span>
          <small v-if="!trash">拖入图片上传到{{ folder.split('/').length === 3 ? '当前笔记目录' : '共用图片' }}</small>
        </div>
        <p v-if="loading" class="search-status">正在读取图片…</p>
        <div v-else-if="visible.length" class="asset-grid">
          <button v-for="asset in visible" :key="asset.id" type="button" :aria-pressed="selected?.id === asset.id" class="asset-card" @click="inspect(asset)">
            <span class="asset-image"><img :src="assetPreviewURL(asset.id, trash)" alt="" loading="lazy" decoding="async"></span>
            <span class="asset-card-caption"><strong :title="asset.id">{{ asset.name }}</strong><small>{{ folderLabel(directory(asset.id)) }}</small></span>
          </button>
        </div>
        <div v-else class="asset-empty"><AppIcon name="image" :size="28" /><p>{{ query || folder ? '没有匹配的图片' : trash ? '回收站为空' : '图库为空' }}</p></div>
        <nav v-if="pageCount > 1" class="admin-pagination" aria-label="图片分页">
          <button type="button" class="button button-quiet" :disabled="page <= 1" @click="page--">上一页</button>
          <span>{{ page }} / {{ pageCount }}</span>
          <button type="button" class="button button-quiet" :disabled="page >= pageCount" @click="page++">下一页</button>
        </nav>
      </div>

      <aside v-if="selected" class="asset-inspector" aria-label="图片详情">
        <header><h2>{{ selected.name }}</h2><button type="button" class="icon-button" aria-label="关闭图片详情" @click="inspect()"><AppIcon name="close" :size="16" /></button></header>
        <a :href="assetPreviewURL(selected.id, trash)" class="asset-image" target="_blank" rel="noopener" aria-label="查看原图">
          <img :src="assetPreviewURL(selected.id, trash)" alt="">
        </a>
        <small class="asset-inspector-path">{{ selected.id }} · {{ Math.max(1, Math.round(selected.bytes / 1024)) }} KB</small>
        <div v-if="!trash" class="asset-actions">
          <button type="button" class="button button-quiet" @click="copy(selected, false)">复制链接</button>
          <button type="button" class="button button-quiet" @click="copy(selected, true)">复制 Markdown</button>
        </div>
        <section class="asset-usage">
          <h3>引用这张图片的笔记</h3>
          <p v-if="usageError" class="error-message">{{ usageError }}</p>
          <p v-else-if="!usage" class="form-hint">正在查找引用…</p>
          <p v-else-if="!usage.length" class="form-hint">当前正文没有引用。</p>
          <RouterLink v-for="note in usage ?? []" :key="note.id" :to="adminEditorPath(note.id)" class="text-link">{{ note.title }} <AppIcon name="arrowRight" :size="13" /></RouterLink>
        </section>
        <button type="button" class="button" :class="trash ? 'button-quiet' : 'button-danger'" :disabled="!!moving || (!trash && !!usage?.length)" @click="changeLocation(selected)">
          {{ trash ? '恢复图片' : '移到回收站' }}
        </button>
      </aside>
    </div>
  </section>
</template>
