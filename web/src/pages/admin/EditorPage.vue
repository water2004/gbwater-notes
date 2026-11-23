<script setup lang="ts">
import { siteBase } from '@/lib/paths'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate, useRoute, useRouter } from 'vue-router'
import { useHead } from '@unhead/vue'
import { useTheme } from '@/lib/theme'
import AppIcon from '@/components/AppIcon.vue'
import EditorImagesPanel from '@/components/EditorImagesPanel.vue'
import RevisionDialog from '@/components/RevisionDialog.vue'
import MarkdownEditor, { type MarkupAction } from '@/components/MarkdownEditor.vue'
import {
  adminEditorPath, createAdminNote, readAdminNote, readNotePreview, updateAdminNote,
} from '@/lib/adminNotes'
import { fetchAdminCategories } from '@/lib/adminCategories'
import { siteSettings } from '@/lib/siteSettings'
import { notePath } from '@/lib/content'
import { createMarkdownRenderer } from '@/lib/markdown'
import { acceptedImages, editorImageId, editorImageURL, imageMarkdown, moveAsset, type AdminAsset } from '@/lib/adminAssets'
import { findImageReferences, imageAltText, type ImageReference, type ImageTarget } from '@/lib/editorImages'
import { useEditorImageUploads } from '@/lib/editorImageUploads'
import { composeMarkdown, downloadMarkdown, splitMarkdown } from '@/lib/editorMarkdown'
import { ApiError } from '@/lib/api'
import { findHeadings, type EditorHeading } from '@/lib/editorSyntax'
import type { NativeDocument } from '@/types/content'

interface LocalDraft {
  title: string
  category: string
  course: string
  slug: string
  courseSlug: string
  order: number
  draft: boolean
  body: string
  frontmatter?: string[]
  queuedTrash?: string[]
  updatedAt: string
}

const route = useRoute()
const router = useRouter()
const { isDark, toggleTheme } = useTheme()
const isNew = computed(() => !route.params.category)
const id = computed(() => isNew.value ? '' : `${route.params.category}/${route.params.course}/${route.params.title}`)
const notesBackPath = computed(() => {
  const from = route.query.from
  return typeof from === 'string' && from.startsWith('/admin/notes?') ? from : '/admin/notes'
})
const title = ref('')
const categoryOptions = ref(siteSettings.categories)
const category = ref(Object.keys(categoryOptions.value)[0] || '')
const storedCategoryName = ref('')
const displayCategoryName = computed(() => categoryOptions.value[category.value]?.name || storedCategoryName.value || category.value)
const course = ref('')
const order = ref(9999)
const draft = ref(true)
const body = ref('')
const slug = ref('')
const courseSlug = ref('')
const revision = ref('')
const native = ref<NativeDocument>({ math: {}, mermaid: {} })
const previewSource = ref('')
const loading = ref(false)
const loaded = ref(false)
const saving = ref(false)
const error = ref('')
const message = ref('')
const lastSavedAt = ref('')
const savedSnapshot = ref('')
const pendingLocalDraft = ref<LocalDraft>()
const draftSavedAt = ref('')
type ViewMode = 'editor' | 'split' | 'preview'
type Inspector = 'images' | 'outline' | 'settings'
const viewMode = ref<ViewMode>('editor')
const compact = ref(false)
const inspector = ref<Inspector>()
const inspectorSurface = ref<'editor' | 'preview'>('editor')
const visibleMode = computed(() => viewMode.value === 'split' && (compact.value || inspector.value)
  ? (inspector.value ? inspectorSurface.value : 'editor')
  : viewMode.value)
let viewport: MediaQueryList | undefined
let inspectorTrigger: HTMLElement | undefined
const inspectorHeading = ref<HTMLElement>()
const syncScroll = ref(true)
const historyOpen = ref(false)
const imageTab = ref<'used' | 'browse' | 'trash'>('used')
const activeImage = ref<number>()
const images = computed(() => findImageReferences(body.value))
const queuedTrash = ref<string[]>([])
const recycledHere = ref<string[]>([])
const unlinkedHere = ref<string[]>([])
const imageRefresh = ref(0)
const movingImage = ref(false)
const imageInput = ref<HTMLInputElement>()
const documentMenu = ref<HTMLDetailsElement>()
const extraToolsMenu = ref<HTMLDetailsElement>()
const conditionTitle = ref<HTMLInputElement>()
let fileSelection: { target: ImageTarget; replacement: boolean } | undefined
const { pending: uploading, failures: uploadFailures, run: runUpload, resolve: resolveUpload, clearFailures } = useEditorImageUploads()
const conflict = ref(false)
const importInput = ref<HTMLInputElement>()
const frontmatter = ref<string[]>([])
const persistedContent = ref('')
let restored: { source: string; snapshot: string } | undefined
let loadVersion = 0
const editor = ref<InstanceType<typeof MarkdownEditor>>()
const preview = ref<HTMLElement>()
const selection = ref({ line: 1, column: 1, offset: 0 })
let draftTimer: ReturnType<typeof setTimeout> | undefined
let previewTimer: ReturnType<typeof setTimeout> | undefined

const toolbar: { action: MarkupAction; label: string; title: string }[] = [
  { action: 'bold', label: 'B', title: '加粗 · Ctrl B' },
  { action: 'italic', label: 'I', title: '斜体 · Ctrl I' },
  { action: 'heading', label: 'H2', title: '二级标题' },
  { action: 'link', label: '链接', title: '插入链接' },
  { action: 'code', label: '</>', title: '行内代码' },
  { action: 'quote', label: '引用', title: '引用块' },
  { action: 'unordered', label: '列表', title: '无序列表' },
  { action: 'codeBlock', label: '代码块', title: '代码块' },
  { action: 'inlineMath', label: 'ƒx', title: '行内公式' },
  { action: 'displayMath', label: '∑', title: '独立公式' },
  { action: 'callout', label: '证明', title: '证明容器' },
  { action: 'table', label: '表格', title: '插入表格' },
  { action: 'divider', label: '分隔', title: '水平分隔线' },
]
const primaryTools = toolbar.filter(item => ['bold', 'italic', 'heading', 'unordered', 'link', 'inlineMath'].includes(item.action))
const dirty = computed(() => savedSnapshot.value !== '' && snapshot() !== savedSnapshot.value)
const statusText = computed(() => saving.value ? '正在保存…' : dirty.value ? '有未保存更改' : lastSavedAt.value ? `已保存 ${lastSavedAt.value}` : '内容已同步')
const assetBase = siteBase
const previewHtml = ref('')
const headings = computed(() => findHeadings(body.value))
const currentHeading = computed(() => {
  const index = headings.value.findIndex(heading => heading.from > selection.value.offset)
  return index === -1 ? headings.value.at(-1) : headings.value[index - 1]
})
useHead({ title: () => `${isNew.value ? '新建笔记' : title.value || '编辑笔记'} · 碧山秋云` })

function currentDraft() {
  return {
    title: title.value, category: category.value, course: course.value, slug: slug.value,
    courseSlug: courseSlug.value, order: order.value, draft: draft.value, body: body.value,
    frontmatter: frontmatter.value,
  }
}
function snapshot() { return JSON.stringify(currentDraft()) }

function applyDocument(content: string) {
  const source = splitMarkdown(content)
  const fields = source.fields
  frontmatter.value = source.lines
  title.value = fields.title || ''
  category.value = fields.category || ''
  course.value = fields.course || ''
  storedCategoryName.value = fields.categoryTitle || ''
  slug.value = fields.slug || ''
  courseSlug.value = fields.courseSlug || ''
  order.value = fields.order === undefined || !Number.isFinite(Number(fields.order)) ? 9999 : Number(fields.order)
  draft.value = fields.draft === 'true'
  body.value = source.body
  previewSource.value = source.body
}

function composedContent() {
  if (restored && restored.snapshot === snapshot()) return restored.source
  if (!dirty.value && persistedContent.value) return persistedContent.value
  return composeMarkdown({
    title: title.value, category: category.value, categoryTitle: displayCategoryName.value,
    course: course.value, slug: slug.value, courseSlug: courseSlug.value, order: order.value, draft: draft.value,
  }, frontmatter.value, body.value)
}
function localDraftKey() { return `gbwater-editor-draft:${id.value || 'new'}` }
function formatTime(value: string) { return new Date(value).toLocaleString('zh-CN', { hour12: false }) }
function checkLocalDraft() {
  pendingLocalDraft.value = undefined
  try {
    const stored = localStorage.getItem(localDraftKey())
    if (!stored) return
    const local = JSON.parse(stored) as LocalDraft
    const { updatedAt: _updatedAt, queuedTrash: pending, ...draft } = local
    if (pending?.length || JSON.stringify({ ...draft, frontmatter: draft.frontmatter ?? frontmatter.value }) !== snapshot()) pendingLocalDraft.value = local
  } catch { /* 浏览器可能禁用本地存储 */ }
}
function restoreLocalDraft() {
  if (!pendingLocalDraft.value) return
  const local = pendingLocalDraft.value
  title.value = local.title; category.value = local.category; course.value = local.course; slug.value = local.slug; courseSlug.value = local.courseSlug
  order.value = local.order; draft.value = local.draft; body.value = local.body; previewSource.value = local.body
  frontmatter.value = local.frontmatter ?? frontmatter.value
  queuedTrash.value = Array.isArray(local.queuedTrash) ? local.queuedTrash.filter(id => typeof id === 'string') : []
  pendingLocalDraft.value = undefined
  message.value = '已恢复浏览器中的本地草稿，保存后才会写入服务器。'
}
function clearLocalDraft(key = localDraftKey()) {
  try { localStorage.removeItem(key) } catch { /* 本地存储不可用 */ }
}
function discardLocalDraft() {
  clearLocalDraft()
  pendingLocalDraft.value = undefined
}
function persistLocalDraft() {
  if ((!dirty.value && !queuedTrash.value.length) || loading.value || saving.value) return
  try {
    localStorage.setItem(localDraftKey(), JSON.stringify({
      ...currentDraft(), queuedTrash: queuedTrash.value, updatedAt: new Date().toISOString(),
    }))
    draftSavedAt.value = new Date().toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })
  } catch { /* 保存到服务器仍可使用 */ }
}
function scheduleDraft() {
  clearTimeout(draftTimer)
  clearTimeout(previewTimer)
  previewTimer = setTimeout(() => { previewSource.value = body.value }, 160)
  if ((dirty.value || queuedTrash.value.length) && !loading.value && !saving.value) draftTimer = setTimeout(persistLocalDraft, 700)
}

async function load() {
  const version = ++loadVersion
  const target = id.value
  loading.value = true
  loaded.value = false
  error.value = ''
  conflict.value = false
  restored = undefined
  pendingLocalDraft.value = undefined
  try {
    const categories = await fetchAdminCategories()
    if (version !== loadVersion) return
    categoryOptions.value = categories
    if (target) {
      const [document, nativeContent] = await Promise.all([
        readAdminNote(target),
        readNotePreview(target).catch(() => ({ math: {}, mermaid: {} })),
      ])
      if (version !== loadVersion) return
      revision.value = document.note.revision
      persistedContent.value = document.content
      applyDocument(document.content)
      slug.value = document.note.slug
      courseSlug.value = document.note.courseSlug
      native.value = nativeContent
    } else {
      title.value = ''
      storedCategoryName.value = ''
      const preset = typeof route.query.category === 'string' ? route.query.category : ''
      category.value = (Object.hasOwn(categories, preset) ? preset : Object.keys(categories)[0]) || ''
      course.value = typeof route.query.course === 'string' ? route.query.course : ''
      slug.value = ''
      courseSlug.value = ''
      order.value = 9999
      draft.value = true
      body.value = ''
      revision.value = ''
      frontmatter.value = []
      previewSource.value = ''
      persistedContent.value = ''
      native.value = { math: {}, mermaid: {} }
    }
    draftSavedAt.value = ''
    lastSavedAt.value = ''
    closeInspector(false)
    savedSnapshot.value = snapshot()
    checkLocalDraft()
    loaded.value = true
  } catch (reason) {
    if (version === loadVersion) error.value = reason instanceof Error ? reason.message : '加载失败'
  } finally {
    if (version === loadVersion) loading.value = false
  }
}

async function save() {
  if (!dirty.value || !loaded.value || loading.value || saving.value || movingImage.value || uploading.value || conflict.value) return
  if (!title.value.trim() || !category.value || !course.value.trim()) {
    conditionTitle.value?.focus()
    error.value = '请填写标题、学科和课程'
    return
  }
  saving.value = true
  error.value = ''
  message.value = ''
  clearTimeout(draftTimer)
  const key = localDraftKey()
  const creating = isNew.value
  try {
    const note = {
      title: title.value.trim(), category: category.value, course: course.value.trim(),
      slug: slug.value.trim(), courseSlug: courseSlug.value.trim(), order: order.value, draft: draft.value,
    }
    const document = creating
      ? await createAdminNote(note, body.value)
      : await updateAdminNote(id.value, composedContent(), revision.value)
    revision.value = document.note.revision
    persistedContent.value = document.content
    restored = undefined
    applyDocument(document.content)
    slug.value = document.note.slug
    courseSlug.value = document.note.courseSlug
    savedSnapshot.value = snapshot()
    clearLocalDraft(key)
    draftSavedAt.value = ''
    lastSavedAt.value = new Date().toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })
    if (creating) await router.replace({ path: adminEditorPath(document.note.id), query: { from: notesBackPath.value } })
    await finishQueuedTrash()
  } catch (reason) {
    conflict.value = reason instanceof ApiError && reason.code === 'revision_conflict'
    error.value = reason instanceof Error ? reason.message : '保存失败'
  } finally {
    saving.value = false
    persistLocalDraft()
  }
}

function reloadAfterConflict() {
  persistLocalDraft()
  queuedTrash.value = []
  void load()
}

async function importMarkdown(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file || saving.value) return
  if (file.size > 5 * 1024 * 1024 || !/\.(md|markdown)$/i.test(file.name)) {
    error.value = '请选择 5 MB 以内的 Markdown 文件'
    return
  }
  if (body.value && !window.confirm('导入会替换编辑器中的正文。继续导入？')) return
  const target = id.value
  try {
    const source = splitMarkdown(await file.text())
    if (target !== id.value) return
    body.value = source.body
    if (isNew.value) {
      title.value = source.fields.title || file.name.replace(/\.[^.]+$/, '')
      if (categoryOptions.value[source.fields.category]) category.value = source.fields.category
      course.value = source.fields.course || course.value
      slug.value = source.fields.slug || ''
      courseSlug.value = source.fields.courseSlug || ''
      if (source.fields.order !== undefined && Number.isInteger(Number(source.fields.order))) {
        order.value = Number(source.fields.order)
      }
    }
    error.value = ''
    message.value = '已导入正文，请检查后保存。'
  } catch {
    error.value = '无法读取 Markdown 文件'
  }
}

function cancelFileSelection() {
  fileSelection?.target.release()
  fileSelection = undefined
}

function chooseImage(reference?: ImageReference) {
  if (saving.value || !imageInput.value) return
  cancelFileSelection()
  const target = editor.value?.captureImageTarget(reference && { from: reference.urlFrom, to: reference.urlTo })
  if (!target) return
  fileSelection = { target, replacement: !!reference }
  imageInput.value.multiple = !reference
  imageInput.value.click()
}

async function receiveFiles(event: Event) {
  const input = event.target as HTMLInputElement
  const files = Array.from(input.files ?? [])
  input.value = ''
  const selection = fileSelection
  fileSelection = undefined
  if (!selection) return
  if (!files.length) { selection.target.release(); return }
  if (!selection.replacement) await revealEditor()
  if (!selection.replacement) editor.value?.focus()
  void runUpload({ ...selection, files, noteId: id.value })
}

function pasteImages(files: File[], target: ImageTarget) {
  void runUpload({ files, target, noteId: id.value, replacement: false })
}

async function insertAsset(asset: AdminAsset) {
  await revealEditor()
  const target = editor.value?.captureImageTarget()
  target?.apply(imageMarkdown(asset), true)
  target?.release()
  editor.value?.focus()
}

function updateViewport() { compact.value = viewport?.matches ?? false }

async function openInspector(panel: Inspector, event?: MouseEvent, surface: 'editor' | 'preview' = 'editor') {
  if (inspector.value !== panel) {
    inspectorTrigger = event?.currentTarget instanceof HTMLElement ? event.currentTarget : undefined
    inspectorSurface.value = surface
    inspector.value = panel
  }
  if (panel === 'images') return
  await nextTick()
  inspectorHeading.value?.focus()
}
function toggleInspector(panel: Inspector, event: MouseEvent) {
  if (inspector.value === panel) closeInspector()
  else void openInspector(panel, event)
}
function openImagePanel(tab: typeof imageTab.value, index?: number) {
  imageTab.value = tab
  if (index !== undefined) activeImage.value = index
  else if (tab === 'used' && activeImage.value === undefined && images.value.length) activeImage.value = 0
}
function toggleImages(event: MouseEvent) {
  if (inspector.value !== 'images') openImagePanel('used')
  toggleInspector('images', event)
}
function closeInspector(returnFocus = true) {
  if (!inspector.value) return
  inspector.value = undefined
  if (returnFocus) {
    if (inspectorTrigger?.getClientRects().length) inspectorTrigger.focus()
    else if (viewMode.value !== 'preview') void nextTick(() => editor.value?.focus())
  }
  inspectorTrigger = undefined
}
function selectView(mode: ViewMode) {
  closeInspector(false)
  viewMode.value = mode
}
async function revealEditor() {
  if (compact.value) closeInspector(false)
  if (viewMode.value === 'preview') viewMode.value = 'editor'
  if (inspector.value) inspectorSurface.value = 'editor'
  await nextTick()
}
async function locateSource(reference: Pick<ImageReference | EditorHeading, 'from' | 'to'>) {
  await revealEditor()
  editor.value?.selectSource(reference.from, reference.to)
}
async function openEditorTool(tool: 'search' | 'complete') {
  await revealEditor()
  editor.value?.[tool]()
}
function openDocumentSettings(event: MouseEvent) {
  if (documentMenu.value) documentMenu.value.open = false
  void openInspector('settings', event)
  inspectorTrigger = documentMenu.value?.querySelector('summary') ?? undefined
}
function openHistory() {
  closeInspector(false)
  if (documentMenu.value) {
    documentMenu.value.open = false
    documentMenu.value.querySelector('summary')?.focus()
  }
  historyOpen.value = true
}
function changeAlt(reference: ImageReference, alt: string) {
  const target = editor.value?.captureImageTarget({ from: reference.altFrom, to: reference.altTo })
  target?.apply(imageAltText(reference, alt))
  target?.release()
}
function removeImage(reference: ImageReference) {
  const target = editor.value?.captureImageTarget(reference)
  if (target?.apply('')) activeImage.value = undefined
  target?.release()
}
function imageReferencesFor(assetId: string) {
  return images.value.filter(image => editorImageId(image.url, assetBase) === assetId)
}
function rememberRecycled(assetId: string) {
  recycledHere.value = [...new Set([...recycledHere.value, assetId])]
}
function cancelImageTrash(assetId: string) {
  queuedTrash.value = queuedTrash.value.filter(id => id !== assetId)
  if (!dirty.value && !queuedTrash.value.length) clearLocalDraft()
}
async function recycleImage(assetId: string, savedHere: boolean) {
  if (saving.value || movingImage.value || uploading.value) return
  error.value = ''
  message.value = ''
  if (savedHere) {
    editor.value?.removeImageReferences(imageReferencesFor(assetId))
    queuedTrash.value = [...new Set([...queuedTrash.value, assetId])]
    activeImage.value = undefined
    return
  }
  movingImage.value = true
  try {
    await moveAsset({ id: assetId }, false)
    editor.value?.removeImageReferences(imageReferencesFor(assetId))
    activeImage.value = undefined
    rememberRecycled(assetId)
    imageRefresh.value++
    message.value = '图片已移到回收站。'
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '回收图片失败'
  } finally { movingImage.value = false }
}
async function finishQueuedTrash() {
  const failures: string[] = []
  for (const assetId of queuedTrash.value) {
    if (imageReferencesFor(assetId).length) continue
    try {
      await moveAsset({ id: assetId }, false)
      rememberRecycled(assetId)
    } catch (reason) {
      failures.push(`${assetId.split('/').at(-1)}：${reason instanceof Error ? reason.message : '回收失败'}`)
    }
  }
  if (failures.length) error.value = `Markdown 已保存，部分图片回收失败。 ${failures.join('；')}`
  queuedTrash.value = []
  imageRefresh.value++
}
function imageRestored(assetId: string) {
  recycledHere.value = recycledHere.value.filter(id => id !== assetId)
  imageRefresh.value++
}
function previewImageClick(event: MouseEvent) {
  const image = event.target
  if (!(image instanceof HTMLImageElement) || !preview.value || image.matches('.math-image,.mermaid-light,.mermaid-dark')) return
  const src = image.getAttribute('src')
  const matches = images.value.map((reference, index) => ({ reference, index })).filter(({ reference }) =>
    (editorImageURL(reference.url, assetBase) ?? reference.url) === src)
  const copies = Array.from(preview.value.querySelectorAll('img')).filter(candidate => candidate.getAttribute('src') === src)
  const match = matches[copies.indexOf(image)] ?? matches[0]
  if (match) {
    void openInspector('images', undefined, 'preview')
    inspectorSurface.value = 'preview'
    openImagePanel('used', match.index)
  }
}

function editorCommand(command: 'bold' | 'italic' | 'save') {
  if (command === 'save') save()
  else format(command)
}
async function format(action: MarkupAction) {
  await revealEditor()
  editor.value?.applyMarkup(action)
}
function syncPreview(ratio: number) {
  if (!syncScroll.value || !preview.value || visibleMode.value !== 'split') return
  const available = preview.value.scrollHeight - preview.value.clientHeight
  preview.value.scrollTop = available * Math.min(1, Math.max(0, ratio))
}
function restoreRevision(source: string, version: number) {
  if (dirty.value && !window.confirm('载入历史版本会替换当前未保存内容。继续载入？')) return
  applyDocument(source)
  restored = { source, snapshot: snapshot() }
  historyOpen.value = false
  message.value = `已载入修订 #${version}。确认内容后再保存。`
}
function keyboard(event: KeyboardEvent) {
  if (event.defaultPrevented || historyOpen.value) return
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 's') {
    event.preventDefault()
    void save()
    return
  }
  if (event.key !== 'Escape') return
  if (documentMenu.value?.open || extraToolsMenu.value?.open) {
    for (const menu of [documentMenu.value, extraToolsMenu.value]) {
      if (!menu?.open) continue
      menu.open = false
      menu.querySelector('summary')?.focus()
    }
  }
  else {
    closeInspector()
  }
}
function closeMenusOutside(event: MouseEvent) {
  if (!(event.target instanceof Node)) return
  for (const menu of [documentMenu.value, extraToolsMenu.value]) {
    if (menu?.open && !menu.contains(event.target)) menu.open = false
  }
}
function beforeUnload(event: BeforeUnloadEvent) {
  if (dirty.value || queuedTrash.value.length || uploading.value || saving.value || movingImage.value) {
    persistLocalDraft()
    event.preventDefault()
  }
}
function canLeave() {
  if (saving.value || uploading.value || movingImage.value) return false
  if (!dirty.value && !queuedTrash.value.length) return true
  persistLocalDraft()
  return window.confirm('还有未保存的修改。确定离开编辑器吗？')
}

watch([body, title, category, course, slug, courseSlug, order, draft, frontmatter, queuedTrash], scheduleDraft)
watch(images, (current, previous) => {
  if (!loaded.value || loading.value) return
  const used = new Set(current.map(image => editorImageId(image.url, assetBase)))
  const unlinked = previous.map(image => editorImageId(image.url, assetBase))
    .filter((assetId): assetId is string => !!assetId && !used.has(assetId))
  if (unlinked.length) unlinkedHere.value = [...new Set([...unlinkedHere.value, ...unlinked])]
  for (const assetId of queuedTrash.value) if (used.has(assetId)) cancelImageTrash(assetId)
})
watch(uploading, (count, previous) => { if (!count && previous) imageRefresh.value++ })
watch([visibleMode, loaded, previewSource, native], () => {
  if (!loaded.value || visibleMode.value === 'editor') return
  previewHtml.value = createMarkdownRenderer(native.value, assetBase, source => editorImageURL(source, assetBase)).render(previewSource.value)
})
watch(previewHtml, async () => {
  await nextTick()
  preview.value?.querySelectorAll('img').forEach(image => { image.loading = 'eager' })
})
watch([visibleMode, syncScroll], async () => {
  if (visibleMode.value !== 'split' || !syncScroll.value) return
  await nextTick()
  syncPreview(editor.value?.scrollRatio() ?? 0)
})
watch(id, () => {
  clearTimeout(draftTimer)
  historyOpen.value = false
  closeInspector(false)
  previewHtml.value = ''
  activeImage.value = undefined
  queuedTrash.value = []
  recycledHere.value = []
  unlinkedHere.value = []
  cancelFileSelection()
  clearFailures()
  void load()
})
onBeforeRouteLeave(canLeave)
onBeforeRouteUpdate(() => saving.value && isNew.value ? true : canLeave())
onMounted(async () => {
  viewport = window.matchMedia('(max-width: 900px)')
  updateViewport()
  viewport.addEventListener('change', updateViewport)
  window.addEventListener('keydown', keyboard)
  window.addEventListener('click', closeMenusOutside)
  window.addEventListener('beforeunload', beforeUnload)
  await load()
})
onBeforeUnmount(() => {
  loadVersion++
  cancelFileSelection()
  clearTimeout(draftTimer)
  clearTimeout(previewTimer)
  window.removeEventListener('keydown', keyboard)
  window.removeEventListener('click', closeMenusOutside)
  window.removeEventListener('beforeunload', beforeUnload)
  viewport?.removeEventListener('change', updateViewport)
})
</script>

<template>
  <div class="editor-page">
    <header class="editor-page-header">
      <div class="editor-heading">
        <RouterLink :to="notesBackPath" class="button button-quiet button-compact editor-back" aria-label="返回笔记管理" title="返回笔记管理"><AppIcon name="arrowLeft" :size="17" /><span>笔记管理</span></RouterLink>
        <div><h1 :title="title">{{ isNew ? '新建笔记' : title || '编辑笔记' }}</h1><p v-if="!isNew">{{ displayCategoryName }} · {{ course }}</p></div>
      </div>
      <div class="editor-header-actions">
        <span v-if="loaded" :class="['editor-save-state', { dirty }]" aria-live="polite">{{ statusText }}</span>
        <details v-if="loaded" ref="documentMenu" class="editor-tool-menu editor-document-menu">
          <summary class="button button-quiet button-compact"><AppIcon name="file" :size="16" /> 文档 <AppIcon name="chevronDown" :size="12" /></summary>
          <div>
            <button type="button" :disabled="saving" @click="openDocumentSettings"><AppIcon name="settings" :size="16" /> 文档设置</button>
            <button v-if="!isNew" type="button" :disabled="saving" @click="openHistory"><AppIcon name="history" :size="16" /> 历史版本</button>
            <RouterLink v-if="!isNew && !draft" :to="notePath(slug)" target="_blank" rel="noopener" @click="documentMenu!.open = false"><AppIcon name="external" :size="16" /> 查看公开页</RouterLink>
            <button type="button" :disabled="saving || movingImage || !!uploading" @click="importInput?.click(); documentMenu!.open = false"><AppIcon name="upload" :size="16" /> 导入 Markdown</button>
            <button type="button" @click="downloadMarkdown(composedContent(), title); documentMenu!.open = false"><AppIcon name="download" :size="16" /> 下载 Markdown</button>
            <button type="button" @click="toggleTheme"><AppIcon :name="isDark ? 'sun' : 'moon'" :size="16" /> {{ isDark ? '切换浅色' : '切换深色' }}</button>
          </div>
        </details>
        <button class="button button-primary button-compact" :disabled="!loaded || !dirty || loading || saving || movingImage || !!uploading || conflict" aria-keyshortcuts="Control+S Meta+S" @click="save"><AppIcon name="check" :size="16" />{{ saving ? '保存中…' : queuedTrash.length ? '保存并回收' : '保存' }}</button>
      </div>
    </header>

    <p v-if="error" class="error-message" role="alert">
      {{ error }} <button v-if="conflict" class="text-link" @click="reloadAfterConflict">重新读取并保留本地草稿</button>
      <button v-else-if="!loaded && !loading" class="text-link" @click="load">重新读取</button>
    </p>
    <p v-if="message" class="editor-notice" role="status">{{ message }}</p>
    <div v-if="pendingLocalDraft" class="local-draft-banner"><AppIcon name="history" :size="19" /><div><strong>发现未保存的本地草稿</strong><p>保存于 {{ formatTime(pendingLocalDraft.updatedAt) }}，可以恢复后继续编辑。</p></div><button class="button button-primary" @click="restoreLocalDraft">恢复草稿</button><button class="button button-quiet" @click="discardLocalDraft">放弃</button></div>
    <p v-if="loading" class="search-status"><span class="loading-spinner" />正在读取 Markdown…</p>

    <div v-else-if="loaded" class="editor-workbench">
      <div v-if="isNew" class="new-note-fields" :inert="saving">
        <label class="form-field new-note-title"><span>标题</span><input ref="conditionTitle" v-model="title" placeholder="为这篇笔记起个名字"></label>
        <label class="form-field"><span>学科</span><select v-model="category"><option v-for="(item, key) in categoryOptions" :key="key" :value="key">{{ item.name }}</option></select></label>
        <label class="form-field"><span>课程</span><input v-model="course" placeholder="例如：数学分析"></label>
      </div>
      <div class="editor-commandbar">
        <div class="editor-tools" role="group" aria-label="编辑工具" :inert="saving || movingImage">
          <div class="editor-tool-group editor-format-tools" role="group" aria-label="常用格式">
            <button v-for="item in primaryTools" :key="item.action" type="button" :title="item.title" :aria-label="item.title" @click="format(item.action)">{{ item.label }}</button>
          </div>
          <details ref="extraToolsMenu" class="editor-tool-menu">
            <summary>格式 <AppIcon name="chevronDown" :size="12" /></summary>
            <div>
              <button v-for="item in toolbar" :key="item.action" type="button" :class="{ 'menu-primary-format': primaryTools.includes(item) }" @click="format(item.action); extraToolsMenu!.open = false">{{ item.title }}</button>
            </div>
          </details>
          <div class="editor-tool-group editor-insert-tools">
            <button type="button" title="插入 Markdown 片段 · Ctrl Space" @click="openEditorTool('complete')"><AppIcon name="plus" :size="16" /> 插入</button>
            <button type="button" aria-label="上传图片" @click="chooseImage()" title="上传图片"><AppIcon name="upload" :size="16" /><span class="editor-mobile-label">上传图片</span></button>
            <input ref="imageInput" type="file" class="visually-hidden" :accept="acceptedImages" multiple tabindex="-1" aria-label="选择图片文件" @change="receiveFiles" @cancel="cancelFileSelection">
          </div>
          <div class="editor-tool-group editor-navigation-tools">
            <button type="button" :aria-expanded="inspector === 'images'" aria-label="图片管理" @click="toggleImages"><AppIcon name="image" :size="16" /><span>图片管理</span><small v-if="images.length" aria-hidden="true">{{ images.length }}</small></button>
            <button type="button" :aria-expanded="inspector === 'outline'" aria-label="大纲" title="大纲" @click="toggleInspector('outline', $event)"><AppIcon name="list" :size="16" /><span class="editor-mobile-label">大纲</span></button>
            <button type="button" aria-label="查找与替换" title="查找与替换 · Ctrl F" @click="openEditorTool('search')"><AppIcon name="search" :size="16" /><span class="editor-mobile-label">查找</span></button>
          </div>
          <input ref="importInput" type="file" class="visually-hidden" accept=".md,.markdown,text/markdown" tabindex="-1" aria-label="选择 Markdown 文件" @change="importMarkdown">
        </div>
        <div class="editor-view-controls">
          <label v-if="visibleMode === 'split'" class="scroll-sync"><input v-model="syncScroll" type="checkbox">同步预览</label>
          <div class="segmented-control" role="group" aria-label="编辑器视图">
            <button type="button" :aria-pressed="viewMode === 'editor' || (compact && viewMode === 'split')" @click="selectView('editor')">编辑</button>
            <button v-if="!compact" type="button" :aria-pressed="viewMode === 'split'" @click="selectView('split')">分栏</button>
            <button type="button" :aria-pressed="viewMode === 'preview'" @click="selectView('preview')">预览</button>
          </div>
        </div>
      </div>

      <div v-if="uploadFailures.length" class="editor-upload-errors" role="alert">
        <div v-for="failure in uploadFailures" :key="failure.id">
          <span>{{ failure.message }}</span>
          <button v-if="failure.retry" type="button" class="text-link" @click="resolveUpload(failure, true)">重试</button>
          <button type="button" class="text-link" @click="resolveUpload(failure, false)">关闭提示</button>
        </div>
      </div>
      <div class="editor-stage" :data-inspector="inspector">
        <div class="editor-panes" :data-mode="visibleMode">
          <section class="editor-pane">
            <MarkdownEditor ref="editor" v-model="body" :read-only="saving || movingImage" placeholder="输入 Markdown。行首输入 / 插入片段，也可以直接粘贴图片。" @images="pasteImages" @command="editorCommand" @selection="selection = $event" @scroll="syncPreview" />
          </section>
          <section class="preview-pane">
            <article ref="preview" class="note-article editor-preview" v-html="previewHtml" @click="previewImageClick" />
          </section>
        </div>

        <EditorImagesPanel v-if="inspector === 'images'" :references="images" :note-id="id" :active="activeImage" :tab="imageTab"
          :busy="saving || movingImage || !!uploading" :refresh="imageRefresh"
          :queued="queuedTrash" :recycled-here="recycledHere" :unlinked-here="unlinkedHere"
          @close="closeInspector()" @tab="openImagePanel" @activate="activeImage = $event" @insert="insertAsset" @upload="chooseImage()"
          @locate="locateSource" @change-alt="changeAlt" @replace="chooseImage" @remove="removeImage"
          @trash="recycleImage" @cancel-trash="cancelImageTrash" @restored="imageRestored" />

        <aside v-if="inspector === 'outline'" class="editor-inspector" aria-labelledby="editor-outline-heading">
          <header><h2 id="editor-outline-heading" ref="inspectorHeading" tabindex="-1">大纲</h2><button type="button" class="icon-button" aria-label="关闭大纲" @click="closeInspector()"><AppIcon name="close" :size="18" /></button></header>
          <ol v-if="headings.length" class="editor-outline">
            <li v-for="heading in headings" :key="heading.from" :style="{ '--heading-level': heading.level }">
              <button type="button" :aria-current="heading === currentHeading ? 'location' : undefined" @click="locateSource(heading)">{{ heading.title }}</button>
            </li>
          </ol>
          <p v-else class="editor-outline-empty">用 # 添加标题后，就能从这里跳到各节。</p>
        </aside>

        <aside v-if="inspector === 'settings'" class="editor-inspector" :inert="saving" aria-labelledby="editor-settings-heading">
          <header><h2 id="editor-settings-heading" ref="inspectorHeading" tabindex="-1">文档设置</h2><button type="button" class="icon-button" aria-label="关闭文档设置" @click="closeInspector()"><AppIcon name="close" :size="18" /></button></header>
          <div class="settings-scroll">
            <label class="form-field"><span>课程内顺序</span><input v-model.number="order" type="number" min="0"></label>
            <label class="form-field"><span>笔记英文 slug</span><input v-model="slug" placeholder="留空则自动生成" pattern="[a-z0-9-]+"></label>
            <label class="form-field"><span>课程英文 slug</span><input v-model="courseSlug" placeholder="留空则自动生成" pattern="[a-z0-9-]+"></label>
            <label class="publish-toggle"><input v-model="draft" type="checkbox"><span><strong>设为草稿</strong><small>草稿不会进入公开目录和搜索索引</small></span></label>
            <div class="document-facts"><div><span>服务端修订</span><code>{{ revision || '尚未生成' }}</code></div><div><span>内容路径</span><code>{{ id || '保存后生成' }}</code></div></div>
            <p v-if="!isNew" class="settings-help">标题、学科与课程跟内容路径绑定。如需调整知识结构，请新建笔记并将旧篇设为草稿。</p>
          </div>
        </aside>
      </div>
      <footer class="editor-statusbar">
        <span>Markdown</span><span>行 {{ selection.line }}，列 {{ selection.column }}</span>
        <span v-if="visibleMode !== 'preview'" class="editor-shortcut"><kbd>/</kbd> 插入片段</span>
        <span v-if="uploading" class="upload-status" role="status">正在上传 {{ uploading }} 张图片，可以继续写作</span>
        <span v-else class="draft-status">{{ draftSavedAt ? `本地草稿 ${draftSavedAt}` : '' }}</span>
        <span class="editor-mobile-state" aria-live="polite">{{ statusText }}</span>
      </footer>

    </div>

    <RevisionDialog v-if="historyOpen" :note-id="id" :title="title"
      :current="dirty ? composedContent() : persistedContent" @close="historyOpen = false" @restore="restoreRevision" />
  </div>
</template>
