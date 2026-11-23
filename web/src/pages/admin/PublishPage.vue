<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useHead } from '@unhead/vue'
import { useRoute } from 'vue-router'
import AppIcon from '@/components/AppIcon.vue'
import BuildConfirmDialog from '@/components/BuildConfirmDialog.vue'
import { buildResultLabel, buildTaskLabel, listBuildHistory, useAdminPublish, type BuildReceipt, type BuildRecord } from '@/lib/adminPublish'

const { status, pending, starting, error, start, refresh } = useAdminPublish()
const history = ref<BuildRecord[]>([])
const historyError = ref('')
const submitError = ref('')
const allOpen = ref(false)
const receipt = ref<BuildReceipt>()
const route = useRoute()
const selectedJob = computed(() => Number(route.query.task) || receipt.value?.id)
const requests = new AbortController()
useHead({ title: '发布 · 碧山秋云' })

function date(value?: string) {
  return value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '—'
}

function submitted(task: BuildReceipt) {
  allOpen.value = false
  receipt.value = task
  submitError.value = ''
}

async function publishChanges() {
  submitError.value = ''
  try {
    submitted(await start({ mode: 'incremental' }))
  } catch (reason) {
    submitError.value = reason instanceof Error ? reason.message : '无法加入发布队列'
  }
}

async function loadHistory() {
  historyError.value = ''
  try {
    history.value = await listBuildHistory(requests.signal)
  } catch (reason) {
    if (!requests.signal.aborted) historyError.value = reason instanceof Error ? reason.message : '读取发布记录失败'
  }
}

watch([() => status.value?.id, () => status.value?.state], () => void loadHistory())
let focusedJob: number | undefined
watch([selectedJob, status, history], async ([id]) => {
  if (id === focusedJob) return
  await nextTick()
  const target = id && document.getElementById(`build-${id}`)
  if (target) {
    focusedJob = id
    target.scrollIntoView({ block: 'nearest' })
  }
})
onMounted(loadHistory)
onBeforeUnmount(() => requests.abort())
</script>

<template>
  <div class="publish-page">
    <header class="admin-page-header">
      <div><h1>发布</h1><p>日常发布只构建有变化的内容，复用已有产物。</p></div>
      <div class="rebuild-actions">
        <button type="button" class="button button-quiet" :disabled="pending || starting || !status?.enabled" @click="allOpen = true">全站重建…</button>
        <button type="button" class="button button-primary" :disabled="pending || starting || !status?.enabled" @click="publishChanges">
          <AppIcon name="build" :size="17" />构建并发布
        </button>
      </div>
    </header>
    <BuildConfirmDialog v-if="allOpen" title="全站重建" action="重建并发布全站" :request="{ mode: 'all' }" @close="allOpen = false" @submitted="submitted">
      <p>重新生成所有已保存笔记的正文、公式和图表，并将公开内容、图片和站点设置发布到网站。</p>
      <p class="form-hint">全站重建可能需要较长时间。任务会按队列顺序执行，你可以继续编辑。</p>
    </BuildConfirmDialog>
    <p v-if="submitError" class="error-message" role="alert">{{ submitError }}</p>
    <p v-if="receipt" class="form-hint" role="status">{{ buildTaskLabel(receipt) }}任务 #{{ receipt.id }} {{ receipt.state === 'queued' ? '已加入队列' : '已启动' }}。</p>
    <p v-if="error" class="error-message" role="alert">{{ error }} <button type="button" class="text-link" @click="refresh">刷新状态</button></p>
    <p v-if="status && !status.enabled" class="form-hint">当前服务未启用构建。</p>
    <section v-if="status" :id="status.id && status.state === 'running' ? `build-${status.id}` : undefined" :class="['admin-panel publish-status', { 'selected-build': status.id && status.id === selectedJob }]">
      <div class="panel-heading"><h2>{{ status.state === 'running' ? '当前任务' : '最近一次任务' }}</h2><span :class="['admin-status', { failed: status.state === 'failed' }]" role="status">{{ buildResultLabel(status) }}</span></div>
      <template v-if="status.id">
        <div class="admin-panel-row"><span :title="status.noteId">{{ buildTaskLabel(status) }}</span><strong>#{{ status.id }}</strong></div>
        <div class="admin-panel-row"><span>开始时间</span><strong>{{ date(status.startedAt) }}</strong></div>
        <div v-if="status.finishedAt" class="admin-panel-row"><span>完成时间</span><strong>{{ date(status.finishedAt) }}</strong></div>
      </template>
      <p v-if="status.state === 'running'" class="form-hint">可以离开此页继续编辑，侧栏会显示发布状态。</p>
      <p v-if="status.error" class="error-message">{{ status.error }}</p>
      <details v-if="status.log" class="build-log" :open="status.state === 'failed'"><summary>构建日志</summary><pre class="admin-log">{{ status.log }}</pre></details>
    </section>
    <section v-if="status?.queue?.length" class="admin-panel publish-history">
      <div class="panel-heading"><h2>等待执行</h2><span class="form-hint">{{ status.queued }} 个任务</span></div>
      <div v-for="task in status.queue" :id="`build-${task.id}`" :key="task.id" :class="['admin-panel-row', { 'selected-build': task.id === selectedJob }]">
        <span :title="task.noteId">{{ buildTaskLabel(task) }}</span><strong>#{{ task.id }}</strong>
      </div>
    </section>
    <section class="admin-panel publish-history">
      <h2>发布记录</h2>
      <p v-if="historyError" class="error-message">{{ historyError }} <button type="button" class="text-link" @click="loadHistory">重新读取</button></p>
      <p v-else-if="!history.length" class="form-hint">暂无发布记录。</p>
      <div v-for="record in history" :id="`build-${record.id}`" :key="record.id" :class="['admin-panel-row', { 'selected-build': record.id === selectedJob }]">
        <span class="build-record">
          <b :title="record.noteId">{{ buildTaskLabel(record) }} <small>#{{ record.id }}</small></b>
          <small>{{ date(record.finishedAt) }}</small>
          <small v-if="record.error" class="error-message">{{ record.error }}</small>
        </span>
        <strong :class="['admin-status', { failed: record.state === 'failed' }]">{{ buildResultLabel(record) }}</strong>
      </div>
    </section>
  </div>
</template>
