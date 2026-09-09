// i18n scaffolding (PLAN.md §11): one dictionary now, more languages later.
const dict = {
  en: {
    buckets: 'Buckets',
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
  },
};

let lang = 'en';
export function setLang(l) { if (dict[l]) lang = l; }
export function t(key) { return dict[lang][key] ?? key; }
