<script setup lang="ts">
import { ref } from 'vue'
import AdminDialog from './AdminDialog.vue'
import { useAdminPublish, type BuildReceipt, type BuildRequest } from '@/lib/adminPublish'

const props = defineProps<{ title: string, action: string, request: BuildRequest }>()
const emit = defineEmits<{ close: [], submitted: [BuildReceipt] }>()
const { start, starting } = useAdminPublish()
const error = ref('')

async function submit() {
  error.value = ''
  try {
    emit('submitted', await start(props.request))
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '无法加入发布队列'
  }
}
</script>

<template>
  <AdminDialog :title="title" class="rebuild-dialog" @close="!starting && emit('close')">
    <div class="rebuild-dialog-body">
      <slot />
      <p v-if="error" class="error-message" role="alert">{{ error }}</p>
      <div class="rebuild-actions">
        <button type="button" class="button button-quiet" autofocus :disabled="starting" @click="emit('close')">取消</button>
        <button type="button" class="button button-primary" :disabled="starting" @click="submit">{{ starting ? '正在提交…' : action }}</button>
      </div>
    </div>
  </AdminDialog>
</template>
