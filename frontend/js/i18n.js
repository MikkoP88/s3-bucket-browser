// i18n (PLAN.md §11): dictionaries per language; new strings default to
// the en text so a partially translated dictionary never shows raw keys.
// The language is auto-detected from the browser (fi for Finnish systems)
// and can be overridden via localStorage 's3b-lang'.
const dict = {
  en: {
    buckets: 'Buckets',
    favorites: 'Favorites',
    objects: 'Objects',
    name: 'Name',
    size: 'Size',
    modified: 'Date modified',
    class: 'Storage class',
    items: 'items',
    item: 'item',
    selected: 'selected',
    uploadHere: 'Drop files to upload here',
    noProfiles: 'No connection profiles yet',
    noProfilesSub: 'Add a profile to connect to Amazon S3, MinIO, R2, Wasabi or any S3-compatible storage.\nOr import your existing ~/.aws/credentials.',
    addProfile: 'Add profile',
    importAws: 'Import ~/.aws/credentials',
    noBuckets: 'No buckets',
    noBucketsSub: 'This connection can see no buckets yet.',
    createBucket: 'Create bucket',
    emptyFolder: 'This folder is empty',
    dropToUpload: 'Drag files here to upload',
    loading: 'Loading…',
    // find dialog
    findTitle: 'Deep search',
    findName: 'Name contains / glob (* ?)',
    findLarger: 'Larger than (e.g. 10MB)',
    findSmaller: 'Smaller than (e.g. 500KB)',
    findOlder: 'Modified more than ago (e.g. 30d)',
    findNewer: 'Modified within (e.g. 24h)',
    findLimit: 'Stop after N matches (0 = all)',
    findStart: 'Search',
    findCancel: 'Cancel',
    findRunning: 'Searching… {matched} match(es) so far',
    findDone: '{matched} match(es) among {scanned} scanned under s3://{bucket}/{prefix}',
    openLocation: 'Open location',
  },
  fi: {
    buckets: 'Bucketit',
    favorites: 'Suosikit',
    objects: 'Objektit',
    name: 'Nimi',
    size: 'Koko',
    modified: 'Muokattu',
    class: 'Tallennusluokka',
    items: 'kohdetta',
    item: 'kohde',
    selected: 'valittu',
    uploadHere: 'Pudota ladattavat tiedostot tähän',
    noProfiles: 'Ei yhteyoprofiileja',
    noProfilesSub: 'Lisää profiili yhdistääksesi Amazon S3:een, MinIOon, R2:een, Wasabiin tai mihin tahansa S3-yhteensopivaan tallennustilaan.\nTai tuo aiemmat ~/.aws/credentials-tunnistetiedot.',
    addProfile: 'Lisää profiili',
    importAws: 'Tuo ~/.aws/credentials',
    noBuckets: 'Ei bucketeja',
    noBucketsSub: 'Tämä yhteys ei näe vielä yhtään bucketia.',
    createBucket: 'Luo bucket',
    emptyFolder: 'Tämä kansio on tyhjä',
    dropToUpload: 'Pudota tiedostot tähän ladattavaksi',
    loading: 'Ladataan…',
    // find dialog
    findTitle: 'Syvä haku',
    findName: 'Nimi sisältää / jokerimerkit (* ?)',
    findLarger: 'Suurempi kuin (esim. 10MB)',
    findSmaller: 'Pienempi kuin (esim. 500KB)',
    findOlder: 'Muokattu yli sitten (esim. 30d)',
    findNewer: 'Muokattu viimeisen (esim. 24h)',
    findLimit: 'Lopeta N osuman jälkeen (0 = kaikki)',
    findStart: 'Hae',
    findCancel: 'Peruuta',
    findRunning: 'Haetaan… {matched} osumaa toistaiseksi',
    findDone: '{matched} osumaa {scanned} läpikäydystä polussa s3://{bucket}/{prefix}',
    openLocation: 'Avaa sijainti',
  },
};

let lang = 'en';

// detectLang picks the initial language: stored choice first, then the
// browser language, then en.
export function detectLang() {
  const saved = localStorage.getItem('s3b-lang');
  if (saved && dict[saved]) return saved;
  const nav = (navigator.language || 'en').slice(0, 2).toLowerCase();
  return dict[nav] ? nav : 'en';
}

export function setLang(l) { if (dict[l]) lang = l; }
export function getLang() { return lang; }
export function languages() { return Object.keys(dict); }

// t translates a key; {placeholder} tokens interpolate from params.
export function t(key, params = {}) {
  let s = dict[lang][key] ?? dict.en[key] ?? key;
  for (const [k, v] of Object.entries(params)) {
    s = s.replaceAll(`{${k}}`, String(v));
  }
  return s;
}
