// Single source of truth for the app's license identity. Every surface
// that shows license information — the About box, Help → License — reads
// these constants instead of hard-coding its own copy. The normative
// documents live at the repo root (LICENSE, NOTICE): change them there
// first, then mirror the identity here once.
export const LICENSE = {
  product: 'S3 Bucket Browser (s3b)',
  holder: 'MikkoP88',
  holderFull: 'Mikko Pesonen (MikkoP88)',
  year: '2026',
  name: 'PolyForm Internal Use License',
  version: '1.0.0',
  url: 'https://polyformproject.org/licenses/internal-use/1.0.0.txt',
  repo: 'https://github.com/MikkoP88/s3-bucket-browser',
};

// One-line form used by the About box and anywhere else a compact
// attribution is needed.
export function licenseLine() {
  return `${LICENSE.name} ${LICENSE.version} — Copyright (c) ${LICENSE.year} ${LICENSE.holder}`;
}
