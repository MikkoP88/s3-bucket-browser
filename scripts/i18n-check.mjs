#!/usr/bin/env node
// Deep validation of frontend/js/i18n.js (runs from scripts/js-check.sh):
//   1. every language dictionary exposes exactly the same key set as en
//   2. every value is a non-empty string
//   3. {placeholder} tokens match the en string's token set exactly
//   4. language codes are 2-letter lowercase and covered by LANG_NAMES
//      (both directions — no stale names either)
//   5. reports strings identical to their en source as INFO (may be
//      legitimate, e.g. de "Name"), never as a failure
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';
import vm from 'node:vm';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const src = readFileSync(join(root, 'frontend/js/i18n.js'), 'utf8')
  .replace(/^export\s+/gm, '');

// Strip ESM exports and evaluate in a sandbox; const/let stay scoped to the
// script, so append a capture line that runs in the same scope.
const sandbox = {};
vm.runInNewContext(`${src}\n;globalThis.__x = { dict, LANG_NAMES };`, sandbox, { filename: 'i18n.js' });
const { dict, LANG_NAMES } = sandbox.__x;

let fail = 0;
const err = (m) => { console.error(`FAIL ${m}`); fail = 1; };

const langs = Object.keys(dict);
if (!langs.includes('en')) err('en dictionary missing');
console.log(`i18n: ${langs.length} languages (${langs.join(', ')}), ${Object.keys(dict.en).length} keys`);

const tokens = (s) => [...String(s).matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort().join(',');

for (const lang of langs) {
  if (!/^[a-z]{2}$/.test(lang)) err(`bad language code: ${lang}`);
  if (!LANG_NAMES || typeof LANG_NAMES[lang] !== 'string' || !LANG_NAMES[lang].trim()) {
    err(`LANG_NAMES missing native name for '${lang}'`);
  }
  if (!(lang in dict)) continue;
  const entries = Object.entries(dict[lang]);
  for (const [k, v] of entries) {
    if (typeof v !== 'string' || v.trim() === '') err(`${lang}.${k}: empty or non-string value`);
    else if (tokens(dict.en[k]) !== tokens(v)) {
      err(`${lang}.${k}: placeholder mismatch (en={${tokens(dict.en[k])}} ${lang}={${tokens(v)}})`);
    }
  }
  const missing = Object.keys(dict.en).filter((k) => !(k in dict[lang]));
  const extra = Object.keys(dict[lang]).filter((k) => !(k in dict.en));
  if (missing.length) err(`${lang}: missing keys (${missing.length}): ${missing.join(', ')}`);
  if (extra.length) err(`${lang}: unknown keys (${extra.length}): ${extra.join(', ')}`);
}

if (LANG_NAMES) {
  const stale = Object.keys(LANG_NAMES).filter((c) => !(c in dict));
  if (stale.length) err(`LANG_NAMES has names for non-existent languages: ${stale.join(', ')}`);
}

const untranslated = [];
for (const lang of langs.filter((l) => l !== 'en')) {
  for (const [k, v] of Object.entries(dict[lang])) {
    if (v === dict.en[k]) untranslated.push(`${lang}.${k}`);
  }
}
if (untranslated.length) {
  console.log(`INFO identical to en (${untranslated.length}): ${untranslated.join(', ')}`);
}

if (fail) {
  console.error('i18n-check: FAILED');
  process.exit(1);
}
console.log('i18n-check: OK');
