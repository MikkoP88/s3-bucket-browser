# CLI quickstart

The CLI ships in the same binary as the GUI — run `s3b` with any
argument. This page is the guided path; the complete syntax for every
command is the [CLI reference](cli.md), and the windowed face of the
same engine is the [GUI usage guide](usage.md). For the binary itself,
see the [releases](https://github.com/MikkoP88/s3-bucket-browser/releases) or [Build from source](build.md).

## Connect and diagnose

```bash
# Connect to any S3 provider (AWS, MinIO, Wasabi, R2, ...) — an S3
# source is one bucket; credentials also fall back to
# $S3B_ACCESS_KEY / $S3B_SECRET_KEY
s3b source add lab s3://my-bucket --endpoint http://localhost:9000 \\
    --access-key minioadmin --secret-key minioadmin
s3b source test lab          # lightweight connectivity check
s3b doctor s3://my-bucket    # deep diagnosis: DNS → TCP → TLS → auth → policy/ACL

# Non-S3 sources live in the same store and use NAME:// URIs everywhere;
# shorthand sftp://user:pass@host:port/root — a password-less URL uses
# your default SSH keys (~/.ssh/id_ed25519 and friends), and --password
# / $S3B_PASSWORD work like on the S3 side
s3b source add vault sftp://deploy@backups.example.com
s3b ls vault://media         # same engine, same flags as s3://

# An account-wide S3 source from an older workspace splits on demand —
# --dry-run previews, --json lists what would be created
s3b source split old-account
```

Sources and legacy profiles are one store: `s3b source list` and
`s3b profile list` show the same connections, and `s3b source remove`
retires one.

## Browse

```bash
s3b ls                        # buckets
s3b ls s3://b/photos/         # one folder view (any source: lab://, vault://)
s3b tree s3://b               # ASCII tree
s3b du s3://b/photos/         # size + count under a prefix
s3b stat s3://b/photos/a.jpg  # object metadata
s3b mb s3://new-bucket        s3b mkdir s3://b/folder/
```

## Transfer

```bash
s3b cp report.pdf s3://b/docs/             # upload
s3b cp -r ./site s3://b/site/              # recursive upload
s3b cp s3://b/docs/report.pdf ./out/       # download
s3b cp s3://b/a.jpg s3://b/copy/a.jpg      # server-side copy
s3b cp -r s3://b/site/ vault://site/       # migrate S3 -> SFTP
s3b mv s3://b/old.txt s3://b/new.txt       # copy, then delete sources on success
s3b sync ./site s3://b/site/ --delete      # one-way sync (either direction);
                                           #   --delete also removes target-side
                                           #   extras (>50 removals needs --force)
s3b presign s3://b/docs/report.pdf --expires 1h
```

## Delete — count first, act second

```bash
s3b rm s3://b/tmp/file.txt                # single object
s3b rm -r --dry-run s3://b/tmp/           # preview a prefix delete
s3b rm -r --force s3://b/tmp/             # >50 objects requires --force
s3b rm -r --versions --force s3://b/tmp/  # destroy all versions too (L3)
s3b rb s3://old-bucket --dry-run          # the removal plan, nothing touched
s3b rb s3://old-bucket --force            # empty + remove (L2; purges
                                          #   version history if versioned)
```

## Object versions (versioned buckets)

```bash
s3b bucket versioning s3://b on           # enable versioning
s3b versions ls s3://b/docs/report.pdf    # timeline, newest first
s3b versions restore s3://b/docs/report.pdf --version-id ID
s3b versions undo s3://b/docs/report.pdf --version-id MARKER  # un-delete
s3b versions stat s3://b                  # current/noncurrent/marker stats
s3b versions purge s3://b --mode noncurrent --dry-run
s3b versions rm s3://b/docs/report.pdf --all               # permanent (L3)
```

## Bucket administration

```bash
s3b bucket info s3://b                    # region, versioning, encryption, PAB
s3b bucket versioning s3://b off
s3b bucket policy put s3://b policy.json  # also: cors | lifecycle |
s3b bucket tags put s3://b team=infra     #      encryption | pab | website
```

## Search

```bash
s3b find s3://b --name 'backup*'          # substring or glob over the key
s3b find s3://b/photos/ --larger 10MB --older 90d
s3b find s3://b --kind file               # only files (kind: file|dir)
s3b find s3://b --ext pdf,csv --path docs # filter by extension and path
s3b find s3://b --class GLACIER --limit 100
```

## Storage-class conversion

```bash
s3b sc s3://b/photos/a.jpg GLACIER          # single object
s3b sc s3://b/photos/ GLACIER -r --dry-run  # whole prefix; >50 needs --force
```

Conversion is a server-side self-copy; reading GLACIER / DEEP_ARCHIVE
objects still needs an explicit restore.

## Object lock

```bash
s3b mb s3://b --object-lock               # the ONLY moment lock can be enabled
s3b bucket lock s3://b --enable --mode GOVERNANCE --days 30   # default retention rule
s3b lock retention s3://b/report.pdf --mode GOVERNANCE --until +7d
s3b lock retention s3://b/report.pdf --clear --bypass-governance
s3b lock legalhold s3://b/report.pdf --on
```

Enabling object lock is permanent; COMPLIANCE retention cannot be
shortened or removed by anyone.

## Every command

Every command takes `--json` for machine-readable output, `--profile`
to pick a connection, and `--verbose` for per-item detail. Shell
completions: `s3b completion bash|zsh|fish|powershell`. Exit codes:
`0` OK, `1` operation failure, `2` usage/config error, `3` unexpected.

## The safety ladder

Destructive operations count first and act second. Prefix deletes over
50 objects require `--force`, removing non-empty buckets requires
`--force`, permanent version destruction is always an explicit choice,
and the GUI routes every delete through the pre-counting Delete Window —
with an optional typed `delete` gate in Settings for extra friction.
See [security.md](security.md#destructive-operations--the-safety-ladder)
for the full ladder.
