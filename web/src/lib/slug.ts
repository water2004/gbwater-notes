// 标题锚点规则同时被 Markdown 渲染和目录使用，单独抽出避免构建脚本引入 Vue 依赖。
export function headingSlug(title: string): string {
  return title.trim().toLowerCase()
    .replace(/[`*_~[\](){}<>!?，。；：、,.:;'"/\\|+=#$%^&]/g, '')
    .replace(/\s+/g, '-')
}
