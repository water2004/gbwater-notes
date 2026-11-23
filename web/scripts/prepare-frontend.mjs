import { existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, renameSync, rmSync, writeFileSync } from 'node:fs'
import { dirname, extname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { apiPlaceholder, deploymentPaths, dynamicAssets, isDynamicPath, sitePlaceholder } from './deployment.ts'

export function prepareFrontend(starter, dist, paths) {
  const source = resolve(starter)
  const files = readdirSync(source, { recursive: true, withFileTypes: true })
    .filter(entry => entry.isFile())
    .map(entry => relative(source, join(entry.parentPath, entry.name)).replaceAll('\\', '/'))
    .filter(name => isDynamicPath(name) || name === 'favicon.svg')
    .sort((a, b) => Number(b.startsWith(`${dynamicAssets}/`)) - Number(a.startsWith(`${dynamicAssets}/`)))
  mkdirSync(dist, { recursive: true })
  for (const name of files) {
    const target = join(dist, name)
    if (name === 'favicon.svg' && existsSync(target)) continue
    let data = readFileSync(join(source, name))
    if (['.html', '.js', '.css'].includes(extname(name))) {
      data = Buffer.from(data.toString('utf8').replaceAll(sitePlaceholder, paths.site).replaceAll(apiPlaceholder, paths.api))
    }
    try {
      if (readFileSync(target).equals(data)) continue
    } catch (error) {
      if (error.code !== 'ENOENT') throw error
    }
    const directory = dirname(target)
    mkdirSync(directory, { recursive: true })
    const temporary = mkdtempSync(join(directory, '.startup-'))
    try {
      const output = join(temporary, 'file')
      writeFileSync(output, data)
      renameSync(output, target)
    } finally {
      rmSync(temporary, { recursive: true, force: true })
    }
  }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  prepareFrontend('/opt/gbwater/frontend', join(import.meta.dirname, '../dist'), deploymentPaths(process.env))
}
