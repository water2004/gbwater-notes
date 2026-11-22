import { markdownLanguage } from '@codemirror/lang-markdown'
import MarkdownIt from 'markdown-it'
import { escapeImageAlt } from './adminAssets'

const { escapeHtml, unescapeAll } = new MarkdownIt().utils

export interface ImageReference {
  from: number
  to: number
  altFrom: number
  altTo: number
  urlFrom: number
  urlTo: number
  alt: string
  url: string
  htmlAlt?: 'missing' | 'quoted' | 'unquoted'
}

export interface ImageTarget {
  apply: (content: string, focus?: boolean) => boolean
  release: () => void
}

export function imageAltText(reference: ImageReference, alt: string) {
  if (!reference.htmlAlt) return escapeImageAlt(alt)
  const escaped = escapeHtml(alt.replaceAll(/\r?\n/g, ' ')).replaceAll("'", '&#39;')
  if (reference.htmlAlt === 'missing') return ` alt="${escaped}"`
  if (reference.htmlAlt === 'unquoted') return `"${escaped}"`
  return escaped
}

function htmlImageReferences(source: string, offset: number): ImageReference[] {
  const references: ImageReference[] = []
  const tags = /<!--[\s\S]*?(?:-->|$)|(<img(?=[\s/>])(?:[^"'<>]|"[^"]*"|'[^']*')*>)/gi
  for (const match of source.matchAll(tags)) {
    if (!match[1]) continue
    const from = offset + match.index
    const attributes = new Map<string, { value: string; from: number; to: number; quoted: boolean }>()
    for (const attr of match[1].matchAll(/\s+([^\s"'<>/=]+)(?:\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'=<>]+)))?/dg)) {
      const name = attr[1].toLowerCase()
      const range = attr.indices?.[2] ?? attr.indices?.[3] ?? attr.indices?.[4]
      if (range && !attributes.has(name)) {
        attributes.set(name, {
          value: unescapeAll(match[1].slice(...range)),
          from: from + range[0], to: from + range[1], quoted: attr[4] === undefined,
        })
      }
    }
    const src = attributes.get('src')
    if (!src?.value) continue
    const alt = attributes.get('alt')
    const tagStart = from + '<img'.length
    references.push({
      from, to: from + match[1].length,
      url: src.value, urlFrom: src.from, urlTo: src.to,
      alt: alt?.value ?? '', altFrom: alt?.from ?? tagStart, altTo: alt?.to ?? tagStart,
      htmlAlt: alt ? (alt.quoted ? 'quoted' : 'unquoted') : 'missing',
    })
  }
  return references
}

export function findImageReferences(body: string) {
  const images: ImageReference[] = []
  markdownLanguage.parser.parse(body).iterate({
    enter({ node }) {
      if (node.name === 'HTMLTag' || node.name === 'HTMLBlock') {
        images.push(...htmlImageReferences(body.slice(node.from, node.to), node.from))
        return false
      }
      if (node.name !== 'Image') return
      const url = node.getChild('URL')
      const [opening, closing] = node.getChildren('LinkMark')
      if (!url || !opening || !closing) return false
      const destination = body.slice(url.from, url.to)
      const bracketed = destination.startsWith('<') && destination.endsWith('>')
      const urlFrom = url.from + Number(bracketed)
      const urlTo = url.to - Number(bracketed)
      const suffix = /^[ \t]*\{(?:[^{}"'\r\n]|"[^"\r\n]*"|'[^'\r\n]*')*\}/.exec(body.slice(node.to, node.to + 512))?.[0]
      const hasAttributes = suffix && /(?:^|[\s{])(?:[.#][\w-]+|[\w-]+=)/.test(suffix)
      images.push({
        from: node.from,
        to: node.to + (hasAttributes ? suffix.length : 0),
        altFrom: opening.to,
        altTo: closing.from,
        urlFrom,
        urlTo,
        alt: body.slice(opening.to, closing.from).replace(/\\([\\[\]])/g, '$1'),
        url: body.slice(urlFrom, urlTo),
      })
      return false
    },
  })
  return images
}
