#!/usr/bin/env node
// provenance.mjs — decode and verify the covert provenance watermarks.
//
// The app hides its creator/license identity inside ordinary comment
// lines of a fixed set of source files, as zero-width characters
// (U+2060 frame, U+200B = 0 bit, U+200C = 1 bit — see
// internal/provenance/provenance.go, the canonical Go codec). This
// script is the JS twin: it decodes every marked file and fails when a
// watermark is missing or drifted, so refactors, formatters and
// "cleanups" cannot strip them silently. The expected payload is built
// from frontend/js/license.js — the app's own identity source — so the
// watermarks and the About box can never disagree.
//
// Usage:
//   node scripts/provenance.mjs check        verify all marked files
//   node scripts/provenance.mjs show <file>  print one file's payload
import { readFileSync } from 'node:fs';
import { LICENSE } from '../frontend/js/license.js';

// Every file that carries a watermark. Keep in step with
// internal/provenance/provenance_test.go.
export const MARKED_FILES = [
  'cmd/s3b/main.go',
  'pkg/api/urls.go',
  'pkg/api/remote.go',
  'pkg/core/remotefs/ftp.go',
  'internal/cli/cli.go',
  'frontend/index.html',
  'frontend/js/main.js',
  'frontend/js/grid.js',
  'frontend/js/license.js',
];

const TAG = 's3b-provenance-v1';
export const PAYLOAD =
  `${TAG} | Copyright (c) ${LICENSE.year} ${LICENSE.holderFull}` +
  ` | ${LICENSE.name} ${LICENSE.version}` +
  ` | github.com/MikkoP88/s3-bucket-browser`;

const FRAME = '\u2060';
const ZERO = '\u200B';
const ONE = '\u200C';

// decode finds the first U+2060 frame in text and returns the payload
// inside it, or null when there is no complete, decodable frame.
export function decode(text) {
  const start = text.indexOf(FRAME);
  if (start < 0) return null;
  const after = text.slice(start + FRAME.length);
  const end = after.indexOf(FRAME);
  if (end < 0) return null;
  const between = after.slice(0, end);
  let out = '';
  let cur = 0;
  let n = 0;
  for (const ch of between) {
    if (ch === ZERO) cur = (cur << 1) | 0;
    else if (ch === ONE) cur = (cur << 1) | 1;
    else return null;
    if (++n === 8) {
      if (cur < 0x20 || cur > 0x7e) return null;
      out += String.fromCharCode(cur);
      cur = 0;
      n = 0;
    }
  }
  if (n !== 0 || out.length === 0) return null;
  return out;
}

const mode = process.argv[2];
if (mode === 'show') {
  const file = process.argv[3];
  if (!file) {
    console.error('usage: node scripts/provenance.mjs show <file>');
    process.exit(2);
  }
  const payload = decode(readFileSync(file, 'utf8'));
  if (!payload) {
    console.error(`${file}: no decodable watermark`);
    process.exit(1);
  }
  console.log(`${file}: ${payload}`);
} else if (mode === 'check') {
  let bad = 0;
  for (const f of MARKED_FILES) {
    const payload = decode(readFileSync(f, 'utf8'));
    if (payload === PAYLOAD) {
      console.log(`PASS ${f}`);
    } else {
      bad++;
      console.error(`FAIL ${f}: ${payload === null ? 'no decodable watermark' : `payload drifted: ${payload}`}`);
    }
  }
  console.log(`${MARKED_FILES.length - bad}/${MARKED_FILES.length} watermarks decode to the identity in frontend/js/license.js`);
  process.exit(bad ? 1 : 0);
} else {
  console.error('usage: node scripts/provenance.mjs check | show <file>');
  process.exit(2);
}
