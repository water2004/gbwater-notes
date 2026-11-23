<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useHead } from '@unhead/vue'
import AppIcon from '@/components/AppIcon.vue'
import { buildStateLabel, listBuildHistory, useAdminPublish, type BuildRecord } from '@/lib/adminPublish'

const { status, pending, starting, error, start, refresh } = useAdminPublish()
const history = ref<BuildRecord[]>([])
const historyError = ref('')
const requests = new AbortController()
useHead({ title: '发布 · 碧山秋云' })

function date(value?: string) {
  return value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '—'
}

async function loadHistory() {
  historyError.value = ''
  try {
    history.value = await listBuildHistory(requests.signal)
  } catch (reason) {
    if (!requests.signal.aborted) historyError.value = reason instanceof Error ? reason.message : '读取发布记录失败'
  }
}

watch(() => [status.value?.id, status.value?.state], () => void loadHistory())
onMounted(loadHistory)
onBeforeUnmount(() => requests.abort())
</script>

<template>
  <div class="publish-page">
    <header class="admin-page-header">
      <div><h1>发布</h1><p>将已保存的笔记、图片和站点设置发布到公开站点。</p></div>
      <button type="button" class="button button-primary" :disabled="pending || starting || !status?.enabled" @click="start">
        <AppIcon name="build" :size="17" />{{ starting ? '正在提交…' : status?.state === 'running' ? '加入发布队列' : '构建并发布' }}
      </button>
    </header>
    <p v-if="error" class="error-message" role="alert">{{ error }} <button type="button" class="text-link" @click="refresh">刷新状态</button></p>
    <p v-if="status && !status.enabled" class="form-hint">当前服务未启用构建。</p>
    <section v-if="status" class="admin-panel publish-status">
      <div class="panel-heading"><h2>当前状态</h2><span :class="['admin-status', { failed: status.state === 'failed' }]" role="status">{{ buildStateLabel[status.state] }}</span></div>
      <div class="admin-panel-row"><span>开始时间</span><strong>{{ date(status.startedAt) }}</strong></div>
      <div class="admin-panel-row"><span>完成时间</span><strong>{{ date(status.finishedAt) }}</strong></div>
      <div v-if="status.queued" class="admin-panel-row"><span>排队中</span><strong>{{ status.queued }} 个任务</strong></div>
      <p v-if="status.state === 'running'" class="form-hint">可以离开此页继续编辑，侧栏会显示发布状态。</p>
      <p v-if="status.error" class="error-message">{{ status.error }}</p>
      <details v-if="status.log" class="build-log" :open="status.state === 'failed'"><summary>构建日志</summary><pre class="admin-log">{{ status.log }}</pre></details>
    </section>
    <section class="admin-panel publish-history">
      <h2>发布记录</h2>
      <p v-if="historyError" class="error-message">{{ historyError }} <button type="button" class="text-link" @click="loadHistory">重新读取</button></p>
      <p v-else-if="!history.length" class="form-hint">暂无发布记录。</p>
      <div v-for="record in history" :key="record.id" class="admin-panel-row">
        <span>{{ date(record.finishedAt) }}<small v-if="record.error"> · {{ record.error }}</small></span>
        <strong :class="['admin-status', { failed: record.state === 'failed' }]">{{ buildStateLabel[record.state] }}</strong>
      </div>
    </section>
  </div>
</template>
