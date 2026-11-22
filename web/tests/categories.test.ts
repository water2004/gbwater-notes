import { describe, expect, it } from 'vitest'
import { readCategories, withCatalogCategories } from '../src/lib/categories'

describe('学科目录', () => {
  it('从站点设置保留新学科与空学科，并能读取已发布笔记的旧学科名称', () => {
    const configured = readCategories({ astronomy: { name: '天文学' }, math: { description: '课程说明' } })
    const categories = withCatalogCategories(configured, [
      { category: 'astronomy', categoryTitle: '旧名' },
      { category: 'languages', categoryTitle: '语言学' },
    ])
    expect(configured.astronomy.name).toBe('天文学')
    expect(configured.math.name).toBe('数学')
    expect(categories.astronomy.name).toBe('天文学')
    expect(categories.languages.name).toBe('语言学')
    expect(readCategories({ '../': { name: '无效路径' } })).not.toHaveProperty('../')
  })
})
