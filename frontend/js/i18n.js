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
    // doctor (v2)
    'doctor.title': 'Connection doctor',
    'doctor.runAll': 'Run all',
    'doctor.rerun': 'Re-run',
    'doctor.notRun': 'not run',
    'doctor.pass': 'pass',
    'doctor.warn': 'warn',
    'doctor.fail': 'fail',
    'doctor.skip': 'skip',
    'doctor.advice': 'Advice',
    'doctor.error': 'Error',
    'doctor.info': 'Info',
    'doctor.summary': '{pass} pass \u00B7 {warn} warn \u00B7 {fail} fail \u00B7 {skip} skip',
    // menu bar
    'menu.file': 'File',
    'menu.edit': 'Edit',
    'menu.view': 'View',
    'menu.help': 'Help',
    'menu.importAws': 'Import AWS credentials\u2026',
    'menu.exit': 'Exit',
    'menu.cut': 'Cut',
    'menu.copy': 'Copy',
    'menu.paste': 'Paste',
    'menu.selectAll': 'Select all',
    'menu.rename': 'Rename',
    'menu.delete': 'Delete',
    'menu.refresh': 'Refresh',
    'menu.theme': 'Toggle theme',
    'menu.panes': 'Toggle panels',
    'menu.filter': 'Focus filter',
    'menu.keys': 'Keyboard map',
    'menu.doctor': 'Doctor\u2026',
    'menu.about': 'About s3b',
    'menu.aboutTitle': 'About s3b',
    'menu.aboutLicense': 'License',
    'menu.aboutUrl': 'Project',
    'menu.log': 'Log area',
    // log drawer
    'log.title': 'Log',
    'log.all': 'All',
    'log.copy': 'Copy',
    'log.clear': 'Clear',
    'log.autoscroll': 'Auto-scroll',
    // auto refresh
    'menu.autorefresh': 'Auto refresh',
    'ar.off': 'Off',
    'ar.focus': 'Refresh on focus',
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
    // doctor (v2)
    'doctor.title': 'Yhteystarkistus',
    'doctor.runAll': 'Aja kaikki',
    'doctor.rerun': 'Aja uudelleen',
    'doctor.notRun': 'ei ajettu',
    'doctor.pass': 'ok',
    'doctor.warn': 'varoitus',
    'doctor.fail': 'virhe',
    'doctor.skip': 'ohitettu',
    'doctor.advice': 'Suositus',
    'doctor.error': 'Virhe',
    'doctor.info': 'Lis\u00e4tietoja',
    'doctor.summary': '{pass} ok \u00B7 {warn} varoitusta \u00B7 {fail} virhett\u00e4 \u00B7 {skip} ohitettu',
    // menu bar
    'menu.file': 'Tiedosto',
    'menu.edit': 'Muokkaa',
    'menu.view': 'N\u00e4yt\u00e4',
    'menu.help': 'Ohje',
    'menu.importAws': 'Tuo AWS-tunnistetiedot\u2026',
    'menu.exit': 'Lopeta',
    'menu.cut': 'Leikkaa',
    'menu.copy': 'Kopioi',
    'menu.paste': 'Liit\u00e4',
    'menu.selectAll': 'Valitse kaikki',
    'menu.rename': 'Nime\u00e4 uudelleen',
    'menu.delete': 'Poista',
    'menu.refresh': 'P\u00e4ivit\u00e4',
    'menu.theme': 'Vaihda teemaa',
    'menu.panes': 'N\u00e4yt\u00e4/Piilota paneelit',
    'menu.filter': 'Siirry suodattimeen',
    'menu.keys': 'N\u00e4pp\u00e4inkartta',
    'menu.doctor': 'Yhteystarkistus\u2026',
    'menu.about': 'Tietoja s3b:st\u00e4',
    'menu.aboutTitle': 'Tietoja s3b:st\u00e4',
    'menu.aboutLicense': 'Lisenssi',
    'menu.aboutUrl': 'Projekti',
    'menu.log': 'Lokialue',
    // log drawer
    'log.title': 'Loki',
    'log.all': 'Kaikki',
    'log.copy': 'Kopioi',
    'log.clear': 'Tyhjenn\u00e4',
    'log.autoscroll': 'Autovieritys',
    // auto refresh
    'menu.autorefresh': 'Automaattinen p\u00e4ivitys',
    'ar.off': 'Pois',
    'ar.focus': 'P\u00e4ivit\u00e4 kohdistettaessa',
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
