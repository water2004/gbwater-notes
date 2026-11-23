<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import AdminDialog from './AdminDialog.vue'
import { importDocumentPackage, maxDocumentPackageSize, previewDocumentPackage, type DocumentPackagePlan } from '@/lib/documentPackages'

const emit = defineEmits<{ close: []; imported: [DocumentPackagePlan] }>()
const file = ref<File>()
const plan = ref<DocumentPackagePlan>()
const checking = ref(false)
const importing = ref(false)
const replace = ref(false)
const error = ref('')
let previewRequest: AbortController | undefined

const hasChanges = computed(() => plan.value &&
  (plan.value.newNotes > 0 || plan.value.replaceNotes > 0 || plan.value.newAssets > 0 || plan.value.categories.length > 0))
const canImport = computed(() => plan.value && hasChanges.value && !plan.value.conflicts.length &&
  (!plan.value.replaceNotes || replace.value))
const actionLabels = { create: '新增', reuse: '已存在', replace: '将替换', conflict: '冲突' }

function choose(event: Event) {
  file.value = (event.currentTarget as HTMLInputElement).files?.[0]
  void checkSelected()
}

async function checkSelected() {
  previewRequest?.abort()
  plan.value = undefined
  replace.value = false
  error.value = ''
  checking.value = false
  if (!file.value) return
  if (file.value.size > maxDocumentPackageSize) {
    error.value = '文档包最大 64 MiB。请拆成几个包再导入。'
    return
  }
  const request = new AbortController()
  previewRequest = request
  checking.value = true
  try {
    const result = await previewDocumentPackage(file.value, request.signal)
    if (!request.signal.aborted) plan.value = result
  } catch (reason) {
    if (!request.signal.aborted) error.value = reason instanceof Error ? reason.message : '无法预览文档包'
  } finally {
    if (!request.signal.aborted) checking.value = false
  }
}

async function confirm() {
  if (!file.value || !plan.value || !canImport.value || importing.value) return
  importing.value = true
  error.value = ''
  try {
    const result = await importDocumentPackage(file.value, plan.value.digest, replace.value)
    emit('imported', result)
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '导入失败'
  } finally {
    importing.value = false
  }
}

onBeforeUnmount(() => previewRequest?.abort())
</script>

<template>
  <AdminDialog title="导入文档包" @close="!importing && emit('close')">
    <form class="package-dialog-body" @submit.prevent="confirm">
      <label class="package-file-label">
        <span>选择本地 ZIP 文档包</span>
        <input type="file" accept=".zip,application/zip" :disabled="importing" @change="choose">
      </label>
      <p v-if="checking" class="search-status" role="status"><span class="loading-spinner" />正在检查文件和冲突…</p>
      <p v-if="error" class="error-message" role="alert">
        {{ error }}
        <button v-if="file && file.size <= maxDocumentPackageSize" type="button" class="text-link" :disabled="checking || importing" @click="checkSelected">重新检查</button>
      </p>
      <template v-if="plan">
        <div class="package-summary" role="status">
          <span>新增 {{ plan.newNotes }} 篇笔记</span>
          <span v-if="plan.replaceNotes">替换 {{ plan.replaceNotes }} 篇笔记</span>
          <span>新增 {{ plan.newAssets }} 张图片</span>
          <span>复用 {{ plan.reusedAssets }} 张图片</span>
        </div>
        <p v-if="plan.categories.length" class="form-hint">新增学科 {{ plan.categories.join('、') }}</p>
        <details v-if="plan.remappedAssets.length || plan.recycledAssets.length" class="package-image-changes">
          <summary>图片变动 · 换用新路径 {{ plan.remappedAssets.length }} 张 · 移至回收站 {{ plan.recycledAssets.length }} 张</summary>
          <ul v-if="plan.remappedAssets.length" aria-label="图片新路径">
            <li v-for="id in plan.remappedAssets" :key="id">新路径 {{ id }}</li>
          </ul>
          <ul v-if="plan.recycledAssets.length" aria-label="将移到回收站的图片">
            <li v-for="id in plan.recycledAssets" :key="id">回收 {{ id }}</li>
          </ul>
        </details>
        <ul v-if="plan.conflicts.length" class="package-conflicts" aria-label="导入冲突">
          <li v-for="item in plan.conflicts" :key="`${item.kind}/${item.id}`">
            <strong>{{ item.id }}</strong><span>{{ item.reason }}</span>
          </li>
        </ul>
        <ul class="package-notes" aria-label="包内笔记">
          <li v-for="{ note, action } in plan.notes" :key="note.id">
            <div>
              <strong>{{ note.title }}</strong>
              <small>{{ note.categoryTitle || note.category }} · {{ note.course }} · {{ note.draft ? '草稿' : '公开' }}</small>
            </div>
            <span class="admin-status" :class="{ failed: action === 'conflict' }">{{ actionLabels[action] }}</span>
          </li>
        </ul>
        <p v-if="plan.conflicts.length" class="form-hint">处理上面的冲突后，重新选择文档包检查。</p>
        <p v-else-if="!hasChanges" class="form-hint">此包的内容已全部存在。</p>
        <label v-if="plan.replaceNotes && !plan.conflicts.length" class="package-replace">
          <input v-model="replace" type="checkbox" :disabled="importing">
          <span>确认替换 {{ plan.replaceNotes }} 篇已有笔记，旧正文保留在版本历史中。</span>
        </label>
      </template>
      <div class="package-actions">
        <button type="button" class="button button-quiet" :disabled="importing" @click="emit('close')">关闭</button>
        <button type="submit" class="button button-primary" :disabled="!canImport || checking || importing">{{ importing ? '正在导入…' : '确认导入' }}</button>
      </div>
    </form>
  </AdminDialog>
</template>
