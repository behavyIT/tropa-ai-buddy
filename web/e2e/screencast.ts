import { writeFileSync, mkdirSync } from 'node:fs'
import { join } from 'node:path'
import type { Page } from '@playwright/test'

/**
 * Записывает кадры страницы через Chrome DevTools Protocol (Page.startScreencast)
 * с метками времени и пишет ffconcat-список для сборки видео системным ffmpeg:
 *   ffmpeg -f concat -safe 0 -i <dir>/frames.ffconcat -vsync vfr -pix_fmt yuv420p out.mp4
 */
export async function startScreencast(page: Page, dir?: string) {
  if (!dir) return { stop: async () => {} }
  mkdirSync(dir, { recursive: true })
  const cdp = await page.context().newCDPSession(page)
  const frames: { file: string; ts: number }[] = []
  cdp.on('Page.screencastFrame', async (f: { data: string; sessionId: number; metadata: { timestamp?: number } }) => {
    const file = `frame-${String(frames.length).padStart(6, '0')}.jpg`
    writeFileSync(join(dir, file), Buffer.from(f.data, 'base64'))
    frames.push({ file, ts: f.metadata.timestamp ?? Date.now() / 1000 })
    await cdp.send('Page.screencastFrameAck', { sessionId: f.sessionId }).catch(() => {})
  })
  await cdp.send('Page.startScreencast', { format: 'jpeg', quality: 85, maxWidth: 1440, maxHeight: 900, everyNthFrame: 1 })
  return {
    stop: async () => {
      await cdp.send('Page.stopScreencast').catch(() => {})
      const end = Date.now() / 1000
      const lines = ['ffconcat version 1.0']
      frames.forEach((fr, i) => {
        const next = i + 1 < frames.length ? frames[i + 1].ts : Math.max(end, fr.ts + 1)
        lines.push(`file '${fr.file}'`, `duration ${Math.max(0.02, next - fr.ts).toFixed(3)}`)
      })
      if (frames.length) lines.push(`file '${frames[frames.length - 1].file}'`)
      writeFileSync(join(dir, 'frames.ffconcat'), lines.join('\n') + '\n')
    },
  }
}
