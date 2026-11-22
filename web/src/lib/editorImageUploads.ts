import { onBeforeUnmount, ref, shallowRef } from 'vue'
import { imageMarkdown, imagePath, uploadAsset } from './adminAssets'
import type { ImageTarget } from './editorImages'

interface Job {
  files: File[]
  noteId: string
  target: ImageTarget
  replacement: boolean
}

interface Failure {
  id: number
  message: string
  retry?: Job
}

export function useEditorImageUploads() {
  const pending = ref(0)
  const failures = shallowRef<Failure[]>([])
  const requests = new AbortController()
  let sequence = 0

  async function run(job: Job) {
    if (!job.files.length) {
      job.target.release()
      return
    }
    pending.value += job.files.length
    const inserted: string[] = []
    const failed: File[] = []
    const messages: string[] = []
    for (const file of job.files) {
      try {
        if (requests.signal.aborted) break
        const asset = await uploadAsset(file, job.noteId, requests.signal)
        inserted.push(job.replacement ? imagePath(asset) : imageMarkdown(asset))
      } catch (reason) {
        if (!requests.signal.aborted) {
          failed.push(file)
          messages.push(`${file.name}：${reason instanceof Error ? reason.message : '上传失败'}`)
        }
      }
    }
    pending.value -= job.files.length
    if (requests.signal.aborted) {
      job.target.release()
      return
    }
    if (inserted.length && !job.target.apply(inserted.join('\n\n'))) {
      messages.push('图片已上传，原位置已经改变。可以从图床重新选用。')
    }
    if (messages.length) failures.value = [...failures.value, {
      id: ++sequence,
      message: messages.join('；'),
      retry: failed.length ? { ...job, files: failed } : undefined,
    }]
    if (!failed.length) job.target.release()
  }

  function resolve(failure: Failure, retry: boolean) {
    failures.value = failures.value.filter(item => item.id !== failure.id)
    if (!failure.retry) return
    if (retry) void run(failure.retry)
    else failure.retry.target.release()
  }

  function clearFailures() {
    for (const failure of failures.value) failure.retry?.target.release()
    failures.value = []
  }

  onBeforeUnmount(() => {
    requests.abort()
    clearFailures()
  })
  return { pending, failures, run, resolve, clearFailures }
}
