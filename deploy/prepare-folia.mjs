import { createHash } from 'node:crypto'
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'

const root = process.argv[2]
const lock = JSON.parse(readFileSync(join(root, 'docs/math-port/evidence/entities-source-lock.json'), 'utf8'))
const npmSources = join(root, 'tools/mathjax-bundle/node_modules/@mathjax/src/ts/util')
const sourceRoot = join(root, '.reference/MathJax-src/ts/util')

// Folia's entity lock records the original CRLF bytes of these TypeScript files.
for (const [name, hash] of Object.entries(lock.source.ts)) {
  const bytes = readFileSync(join(npmSources, name), 'utf8').replace(/\r?\n/g, '\r\n')
  if (createHash('sha256').update(bytes).digest('hex') !== hash) throw new Error(`Folia entity hash mismatch: ${name}`)
  mkdirSync(sourceRoot, { recursive: true })
  writeFileSync(join(sourceRoot, name), bytes)
}
