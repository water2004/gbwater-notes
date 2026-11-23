<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useHead } from '@unhead/vue'
import { useRoute } from 'vue-router'
import AppIcon from '@/components/AppIcon.vue'
import DocumentPackageDialog from '@/components/DocumentPackageDialog.vue'
import { adminEditorPath, listAdminNotes, noteDownloadURL, setAdminNoteDraft, type AdminNote } from '@/lib/adminNotes'
import { ApiError } from '@/lib/api'
import { fetchAdminCategories } from '@/lib/adminCategories'
import { withCatalogCategories } from '@/lib/categories'
import { siteSettings } from '@/lib/siteSettings'
import { notePath } from '@/lib/content'
import { downloadDocumentPackage, type DocumentPackagePlan } from '@/lib/documentPackages'

const configured = ref(siteSettings.categories)
const route = useRoute()
const categoryOptions = computed(() => withCatalogCategories(configured.value, notes.value))
const notes = ref<AdminNote[]>([])
const query = ref('')
const category = ref('')
const course = ref('')
const visibility = ref('')
const sort = ref('modified')
const page = ref(1)
const pageSize = 30
const error = ref('')
const notice = ref('')
const busy = ref('')
const loading = ref(true)
const selected = ref<Set<string>>(new Set())
const exporting = ref(false)
const showImport = ref(false)
useHead({ title: '笔记管理 · 碧山秋云' })

const courses = computed(() => [...new Set(notes.value
  .filter(note => !category.value || note.category === category.value)
  .map(note => note.course))].sort((a, b) => a.localeCompare(b, 'zh-CN')))

const filtered = computed(() => {
  const needle = query.value.trim().toLocaleLowerCase('zh-CN')
  return notes.value.filter(note => {
    const matchesQuery = !needle || `${note.title} ${note.course} ${note.slug}`.toLocaleLowerCase('zh-CN').includes(needle)
    return matchesQuery && (!category.value || note.category === category.value)
      && (!course.value || note.course === course.value)
      && (!visibility.value || (visibility.value === 'draft' ? note.draft : !note.draft))
  }).sort((a, b) => {
    if (sort.value === 'title') return a.title.localeCompare(b.title, 'zh-CN')
    if (sort.value === 'course') return a.course.localeCompare(b.course, 'zh-CN') || a.order - b.order
    if (sort.value === 'order') return a.order - b.order || a.title.localeCompare(b.title, 'zh-CN')
    return new Date(b.modified).getTime() - new Date(a.modified).getTime()
  })
})
const pageCount = computed(() => Math.max(1, Math.ceil(filtered.value.length / pageSize)))
const paginated = computed(() => filtered.value.slice((page.value - 1) * pageSize, page.value * pageSize))
const rangeStart = computed(() => filtered.value.length ? (page.value - 1) * pageSize + 1 : 0)
const rangeEnd = computed(() => Math.min(page.value * pageSize, filtered.value.length))
const pageSelected = computed(() => paginated.value.length > 0 && paginated.value.every(note => selected.value.has(note.id)))
watch([query, category, course, visibility, sort], () => { page.value = 1 })
watch(() => route.query, params => {
  query.value = typeof params.q === 'string' ? params.q : ''
  category.value = typeof params.category === 'string' ? params.category : ''
  course.value = typeof params.course === 'string' ? params.course : ''
  visibility.value = params.status === 'draft' || params.status === 'published' ? params.status : ''
  sort.value = params.sort === 'title' || params.sort === 'course' || params.sort === 'order' ? params.sort : 'modified'
}, { immediate: true })

const returnPath = computed(() => {
  const params = new URLSearchParams()
  if (query.value.trim()) params.set('q', query.value.trim())
  if (category.value) params.set('category', category.value)
  if (course.value) params.set('course', course.value)
  if (visibility.value) params.set('status', visibility.value)
  if (sort.value !== 'modified') params.set('sort', sort.value)
  return params.size ? `/admin/notes?${params}` : '/admin/notes'
})
watch(pageCount, count => { if (page.value > count) page.value = count })

async function loadNotes() {
  try {
    const [items, categories] = await Promise.all([listAdminNotes(), fetchAdminCategories()])
    notes.value = items
    configured.value = categories
  }
  catch (reason) { error.value = reason instanceof Error ? reason.message : '加载失败' }
  finally { loading.value = false }
}
onMounted(loadNotes)
function resetFilters() { query.value = ''; category.value = ''; course.value = ''; visibility.value = ''; sort.value = 'modified' }

function selectNotes(items: AdminNote[], checked: boolean) {
  const next = new Set(selected.value)
  for (const note of items) {
    if (checked) next.add(note.id)
    else next.delete(note.id)
  }
  selected.value = next
}

async function exportSelection() {
  const ids = [...selected.value]
  if (!ids.length || exporting.value) return
  exporting.value = true
  error.value = ''
  try {
    const filename = ids.length === 1 ? notes.value.find(note => note.id === ids[0])?.title : undefined
    await downloadDocumentPackage(ids, filename || 'GBwater-笔记')
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '下载文档包失败'
  } finally {
    exporting.value = false
  }
}

async function imported(plan: DocumentPackagePlan) {
  showImport.value = false
  error.value = ''
  const replaced = plan.replaceNotes ? `，替换 ${plan.replaceNotes} 篇` : ''
  notice.value = `已新增 ${plan.newNotes} 篇笔记${replaced}，登记 ${plan.newAssets} 张图片。`
  await loadNotes()
}

async function changeDraft(note: AdminNote) {
  if (busy.value) return
  const action = note.draft ? '设为公开' : '转为草稿'
  if (!window.confirm(`将《${note.title}》${action}？公开站点会在下次构建后更新。`)) return
  busy.value = note.id
  error.value = ''
  notice.value = ''
  try {
    const saved = await setAdminNoteDraft(note, !note.draft)
    notes.value = notes.value.map(item => item.id === saved.id ? saved : item)
    notice.value = `《${note.title}》已${action}。构建后更新公开站点。`
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '更新笔记状态失败'
    if (reason instanceof ApiError && reason.code === 'revision_conflict') {
      try { notes.value = await listAdminNotes() } catch { /* 保留冲突提示 */ }
    }
  } finally {
    busy.value = ''
  }
}
</script>

<template>
  <div>
    <header class="admin-page-header">
      <div><h1>笔记管理</h1><p>查找、编辑和下载笔记，或通过文档包迁移 Markdown 和配图。</p></div>
      <div class="package-actions">
        <button type="button" class="button button-quiet" @click="showImport = true">导入文档包</button>
        <RouterLink :to="{ path: '/admin/editor', query: { from: returnPath, category: category || undefined, course: course || undefined } }" class="button button-primary"><AppIcon name="plus" />新建笔记</RouterLink>
      </div>
    </header>
    <p v-if="error" class="error-message">{{ error }}</p>
    <p v-if="notice" class="editor-notice" role="status">{{ notice }} <RouterLink to="/admin/publish">前往发布</RouterLink></p>
    <div class="admin-toolbar notes-toolbar">
      <label class="admin-filter-search"><AppIcon name="search" :size="17" /><input v-model="query" type="search" placeholder="搜索标题、课程或 slug" aria-label="搜索笔记"></label>
      <select v-model="category" aria-label="按学科筛选" @change="course = ''"><option value="">全部学科</option><option v-for="(item, id) in categoryOptions" :key="id" :value="id">{{ item.name }}</option></select>
      <select v-model="course" aria-label="按课程筛选"><option value="">全部课程</option><option v-for="item in courses" :key="item">{{ item }}</option></select>
      <select v-model="visibility" aria-label="按公开状态筛选"><option value="">全部状态</option><option value="published">公开</option><option value="draft">草稿</option></select>
      <select v-model="sort" aria-label="排序方式"><option value="modified">最近修改</option><option value="title">标题</option><option value="course">课程与顺序</option><option value="order">顺序号</option></select>
      <button v-if="query || category || visibility" class="button button-quiet" @click="resetFilters">清除筛选</button>
    </div>
    <div v-if="!loading && (filtered.length || selected.size)" class="package-selection">
      <span aria-live="polite">已选 {{ selected.size }} 篇</span>
      <button v-if="filtered.length" type="button" class="text-link" @click="selectNotes(filtered, true)">选择当前筛选的 {{ filtered.length }} 篇</button>
      <button v-if="selected.size" type="button" class="text-link" @click="selected = new Set()">清空选择</button>
      <button type="button" class="button button-quiet" :disabled="!selected.size || exporting" @click="exportSelection">
        <AppIcon name="download" :size="16" />{{ exporting ? '正在打包…' : '下载文档包' }}
      </button>
    </div>
    <div class="notes-list-meta"><span>筛选出 {{ filtered.length }} 篇，共 {{ notes.length }} 篇</span><span v-if="filtered.length">显示 {{ rangeStart }}–{{ rangeEnd }}</span></div>
    <p v-if="loading" class="search-status"><span class="loading-spinner" />正在读取内容…</p>
    <div v-else-if="paginated.length" class="admin-table-wrap">
      <table class="admin-table notes-table">
        <thead><tr><th class="package-checkbox"><input type="checkbox" :checked="pageSelected" aria-label="选择当前页笔记"
          @change="selectNotes(paginated, ($event.currentTarget as HTMLInputElement).checked)"></th><th>标题</th><th>课程</th><th>顺序</th><th>状态</th><th>最后修改</th><th>操作</th></tr></thead>
        <tbody><tr v-for="note in paginated" :key="note.id">
          <td class="package-checkbox" data-label="选择"><input type="checkbox" :checked="selected.has(note.id)" :aria-label="`选择 ${note.title}`"
            @change="selectNotes([note], ($event.currentTarget as HTMLInputElement).checked)"></td>
          <td data-label="标题"><RouterLink :to="{ path: adminEditorPath(note.id), query: { from: returnPath } }" class="note-edit-link"><strong>{{ note.title }}</strong><small>{{ note.slug }}</small></RouterLink></td>
          <td data-label="课程"><span>{{ note.course }}</span><small class="table-secondary">{{ categoryOptions[note.category]?.name || note.categoryTitle }}</small></td>
          <td data-label="顺序">{{ note.order }}</td>
          <td data-label="状态"><span :class="['admin-status', { failed: note.draft }]">{{ note.draft ? '草稿' : '公开' }}</span></td>
          <td data-label="最后修改"><time :datetime="note.modified">{{ new Date(note.modified).toLocaleDateString('zh-CN') }}</time></td>
          <td data-label="操作"><div class="table-actions note-file-actions">
            <RouterLink :to="{ path: adminEditorPath(note.id), query: { from: returnPath } }">编辑</RouterLink>
            <a :href="noteDownloadURL(note.id)" :aria-label="`下载 ${note.title} 的 Markdown`" title="下载 Markdown"><AppIcon name="download" :size="15" /></a>
            <button type="button" class="text-link" :disabled="!!busy" @click="changeDraft(note)">{{ note.draft ? '设为公开' : '转为草稿' }}</button>
            <RouterLink v-if="!note.draft" :to="notePath(note)" target="_blank" aria-label="查看公开页" title="查看公开页"><AppIcon name="external" :size="14" /></RouterLink>
          </div></td>
        </tr></tbody>
      </table>
    </div>
    <div v-else-if="!loading" class="empty-state"><AppIcon name="search" :size="30" /><h2>没有匹配的笔记</h2><p>调整关键词或清除筛选后再试。</p><button class="button button-quiet" @click="resetFilters">清除筛选</button></div>
    <nav v-if="pageCount > 1" class="admin-pagination" aria-label="笔记列表分页">
      <button class="button button-quiet" :disabled="page <= 1" @click="page--"><AppIcon name="arrowLeft" :size="15" />上一页</button>
      <span>第 {{ page }} / {{ pageCount }} 页</span>
      <button class="button button-quiet" :disabled="page >= pageCount" @click="page++">下一页<AppIcon name="arrowRight" :size="15" /></button>
    </nav>
    <DocumentPackageDialog v-if="showImport" @close="showImport = false" @imported="imported" />
  </div>
</template>
