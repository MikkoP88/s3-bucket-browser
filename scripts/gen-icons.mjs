#!/usr/bin/env node
// gen-icons rasterizes the brand master (build/icon.svg) into the PNG set
// every platform asset is derived from (build/iconset/icon_<size>.png) and
// the copy the frontend serves (frontend/assets/logo.svg).
//
// Rendering goes through headless Chromium (same playwright-core install the
// visual harness uses) so the SVG is rasterized by a real engine with proper
// alpha, not a Node SVG library. Output is deterministic for a given master.
//
// Usage: node scripts/gen-icons.mjs   (run after regenerating build/icon.svg)

import { mkdirSync, copyFileSync, writeFileSync, readFileSync } from 'node:fs';
import { dirname } from 'node:path';
import { chromium } from 'playwright-core';

const MASTER = 'build/icon.svg';
const ICONSET_DIR = 'build/iconset';
// Sizes Windows, macOS and web contexts ask for (ico takes 16-256, icns the
// power-of-two ladder, frontend/misc the rest).
const SIZES = [16, 24, 32, 40, 48, 64, 96, 128, 192, 256, 512, 1024];
// Fraction of the canvas the mark fills; folder icons breathe a little.
const FILL = 0.94;

async function renderSet() {
  const svg = readFileSync(MASTER, 'utf8');
  const channels = ['msedge', 'chrome', 'chromium'];
  let browser = null;
  for (const c of channels) { try { browser = await chromium.launch({ channel: c }); break; } catch {} }
  if (!browser) throw new Error('no chromium channel available (tried ' + channels.join(', ') + ')');
  try {
    const page = await browser.newPage({ viewport: { width: 512, height: 512 } });
    return await page.evaluate(async ({ svgText, sizes, fill }) => {
      const svg1 = svgText.replace(/<!--[\s\S]*?-->\s*/, '');
      const img = new Image();
      await new Promise((res, rej) => {
        img.onload = res;
        img.onerror = () => rej(new Error('svg failed to load'));
        img.src = 'data:image/svg+xml;base64,' + btoa(unescape(encodeURIComponent(svg1)));
      });
      const vb = /viewBox="0 0 (\d+(?:\.\d+)?) (\d+(?:\.\d+)?)"/.exec(svg1);
      const [vw, vh] = vb ? [Number(vb[1]), Number(vb[2])] : [img.width, img.height];
      const out = {};
      for (const s of sizes) {
        const c = document.createElement('canvas');
        c.width = s; c.height = s;
        const ctx = c.getContext('2d');
        ctx.imageSmoothingEnabled = true;
        ctx.imageSmoothingQuality = 'high';
        const dw = s * fill, dh = dw * vh / vw;
        ctx.drawImage(img, (s - dw) / 2, (s - dh) / 2, dw, dh);
        out[s] = c.toDataURL('image/png');
      }
      return out;
    }, { svgText: svg, sizes: SIZES, fill: FILL });
  } finally {
    await browser.close();
  }
}

const set = await renderSet();
mkdirSync(ICONSET_DIR, { recursive: true });
for (const s of SIZES) {
  writeFileSync(`${ICONSET_DIR}/icon_${s}.png`, Buffer.from(set[s].split(',')[1], 'base64'));
  console.log(`icon_${s}.png`);
}
mkdirSync(dirname('frontend/assets/logo.svg'), { recursive: true });
copyFileSync(MASTER, 'frontend/assets/logo.svg');
console.log('frontend/assets/logo.svg');
