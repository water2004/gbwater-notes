/// <reference types="vite/client" />

declare module '*.vue' {
  import type { DefineComponent } from 'vue'
  const component: DefineComponent<object, object, unknown>
  export default component
}

declare module 'markdown-it-attrs' {
  import type { MarkdownIt } from 'markdown-it'
  export default function attrs(md: MarkdownIt): void
}

declare module 'markdown-it-footnote' {
  import type { MarkdownIt } from 'markdown-it'
  export default function footnote(md: MarkdownIt): void
}

declare module 'markdown-it-container' {
  import type { MarkdownIt, RendererRule } from 'markdown-it'
  export default function container(md: MarkdownIt, name: string, options: { render: RendererRule }): void
}
