import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

function read(relative: string) {
  return readFileSync(new URL(relative, import.meta.url), 'utf8')
}

// 这些断言守护"公开产物里不出现内容源"这一约束：
// 一旦有人把 Markdown 或公式元数据重新 glob 进客户端，测试会先失败。
describe('公开产物不包含内容源', () => {
  it('笔记页不再打包原始 Markdown', () => {
    const source = read('../src/pages/NotePage.vue')
    expect(source).not.toContain('?raw')
    expect(source).not.toContain('content/**/*.md')
  })

  it('后台预览通过受保护接口读取公式元数据', () => {
    const source = read('../src/pages/admin/EditorPage.vue')
    expect(source).not.toContain('generated/notes/**/*.json')
    expect(source).toContain('readNotePreview(target)')
    expect(read('../src/lib/adminNotes.ts')).toContain('api<NativeDocument>(`/admin/note/native?')
  })

  it('笔记片段由 Go/Folia 生成，Vite 不再渲染 Markdown', () => {
    expect(read('../vite.config.ts')).not.toContain('configureServer')
    expect(read('../src/pages/NotePage.vue')).not.toContain('node:')
    expect(read('../src/pages/NotePage.vue')).not.toContain('import.meta.env.DEV')
    expect(read('../../internal/render/html.go')).toContain('notes-html')
  })
})
