<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import AdminDialog from './AdminDialog.vue'
import { listNoteRevisions, readNoteRevision, type RevisionRecord } from '@/lib/adminNotes'
import { compareMarkdown, downloadMarkdown } from '@/lib/editorMarkdown'

const props = defineProps<{ noteId: string; title: string; current: string }>()
const emit = defineEmits<{ close: []; restore: [source: string, revision: number] }>()
const list = ref<RevisionRecord[]>([])
const selected = ref<RevisionRecord>()
const content = ref<string>()
const loading = ref(false)
const error = ref('')
const mode = ref<'diff' | 'source'>('diff')
let request: AbortController | undefined
const diff = computed(() => content.value === undefined || mode.value !== 'diff' ? undefined : compareMarkdown(content.value, props.current))
const labels = { create: '创建', baseline: '编辑前快照', update: '保存' }

function startRequest() {
  request?.abort()
  request = new AbortController()
  loading.value = true
  error.value = ''
  return request.signal
}

async function show(record: RevisionRecord) {
  const signal = startRequest()
  selected.value = record
  content.value = undefined
  try {
    const source = await readNoteRevision(props.noteId, record.id, signal)
    if (!signal.aborted) content.value = source
  } catch (reason) {
    if (!signal.aborted) error.value = reason instanceof Error ? reason.message : '读取快照失败'
  } finally {
    if (!signal.aborted) loading.value = false
  }
}

onMounted(async () => {
  const signal = startRequest()
  try {
    const records = await listNoteRevisions(props.noteId, signal)
    if (signal.aborted) return
    list.value = records
  } catch (reason) {
    if (!signal.aborted) error.value = reason instanceof Error ? reason.message : '读取版本失败'
  } finally {
    if (!signal.aborted) loading.value = false
  }
})
onBeforeUnmount(() => request?.abort())
</script>

<template>
  <AdminDialog title="历史版本" @close="emit('close')">
    <div class="history-layout">
      <aside class="revision-list" aria-label="版本列表">
        <p v-if="!list.length && loading" class="search-status">读取版本中…</p>
        <p v-else-if="!list.length && !error" class="drawer-empty">还没有历史修订。</p>
        <button v-for="record in list" :key="record.id" type="button" :aria-pressed="selected?.id === record.id" :class="{ active: selected?.id === record.id }" @click="show(record)">
          <span><strong>{{ labels[record.action] }} · #{{ record.id }}</strong><small>{{ new Date(record.createdAt).toLocaleString('zh-CN', { hour12: false }) }}</small></span>
        </button>
      </aside>
      <section class="history-detail" aria-label="版本内容">
        <p v-if="error" class="error-message" role="alert">{{ error }}</p>
        <p v-else-if="loading && selected" class="search-status">正在读取快照…</p>
        <p v-else-if="!selected" class="history-placeholder">选择一个版本，与编辑器中的 Markdown 对比。</p>
        <template v-else-if="content !== undefined">
          <div class="history-toolbar">
            <div class="segmented-control" role="group" aria-label="版本查看方式">
              <button type="button" :aria-pressed="mode === 'diff'" @click="mode = 'diff'">对比当前</button>
              <button type="button" :aria-pressed="mode === 'source'" @click="mode = 'source'">历史源码</button>
            </div>
            <button class="button button-quiet" type="button" @click="downloadMarkdown(content, `${title}-${selected.id}`)">下载</button>
            <button class="button button-primary" type="button" @click="emit('restore', content, selected.id)">载入编辑器</button>
          </div>
          <template v-if="diff">
            <p v-if="!diff.added && !diff.removed" class="history-placeholder">此版本与当前内容相同。</p>
            <template v-else>
              <p class="history-summary">当前内容相对于此版本新增 {{ diff.added }} 行，删除 {{ diff.removed }} 行。</p>
              <pre class="history-diff"><span v-for="(line, index) in diff.changes" :key="index" :class="line.kind"><i aria-hidden="true">{{ line.kind === 'added' ? '+' : line.kind === 'removed' ? '−' : ' ' }}</i>{{ line.text }}</span></pre>
              <p v-if="diff.truncated" class="form-hint">对比仅显示前 800 行，可下载完整快照。</p>
            </template>
          </template>
          <pre v-else class="history-source">{{ content }}</pre>
        </template>
      </section>
    </div>
  </AdminDialog>
</template>
