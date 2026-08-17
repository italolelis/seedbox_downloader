# Paths: what gets written, and what gets advertised

This service does two things with paths, and they have to agree:

1. It **writes** downloaded content into `DOWNLOAD_DIR` on local disk.
2. It **advertises** a path over the Transmission RPC, which Sonarr and Radarr use
   to find that content and import it.

When those two disagree, downloads succeed and imports never happen. The \*arr app
polls the advertised path, finds nothing, and leaves the release in its queue
indefinitely with `No files found are eligible for import in <path>`.

Both are now derived from the same place, so they cannot drift apart.

## The contract

| | Value |
|---|---|
| Advertised download directory | `DOWNLOAD_DIR`, verbatim |
| Advertised name | The name of the entry written directly inside `DOWNLOAD_DIR` |

Sonarr and Radarr join those two and look for the result on disk — first as a
directory, then as a file. There is no branching on whether the release is one file
or many, so a correct path is all that is required for either shape.

## How the name is chosen

The name comes from **the file names the seedbox actually stored**, never from the
transfer's name. Those two are frequently different, and the transfer name is the
one that's wrong.

### Single-file transfers

A transfer whose payload is one file writes that file **directly into
`DOWNLOAD_DIR`**, with no enclosing folder:

```
DOWNLOAD_DIR/Silo.S03E07.1080p.DUAL-SiGLA.mkv
```

This matches what Transmission does with a single-file torrent, which is what the
\*arr apps are built to expect. Earlier versions invented a wrapper folder named
after the file with its extension stripped — `Silo.S03E07.1080p.DUAL-SiGLA/` — which
no other client would create and which nothing advertised.

### Folder transfers

A transfer whose payload is a folder writes that folder, with its structure intact:

```
DOWNLOAD_DIR/Show.S01.1080p-GROUP/Season 1/s01e01.mkv
DOWNLOAD_DIR/Show.S01.1080p-GROUP/Season 1/s01e02.mkv
```

The advertised name is the outermost folder — `Show.S01.1080p-GROUP`.

### Collision suffixes

When a name already exists on the seedbox account, Put.io appends a short id to
keep them distinct:

```
transfer name:  Silo S03E08 HiggsBoson .exe
stored file:    Silo S03E08 HiggsBoson  ojqRfI77.exe
                                       ^^^^^^^^
```

The suffix is assigned *after* the transfer is created, so the transfer name can
never contain it. Anything derived from the transfer name is therefore guaranteed
wrong for these. Deriving from the stored file name gets it right, and this matters
most on **shared accounts**, where several people pull the same release into
different folders and collisions are constant.

The seedbox may also rename a file entirely — a `-BTM` release arriving as a
`[Ben The Men]` rename. The stored name still wins.

### In-progress transfers

Before a transfer completes, its files are not discoverable, so there is nothing to
derive a name from. In that window the transfer name is advertised as a fallback and
a warning is logged. Nothing has been written to disk yet either, so nothing can be
imported regardless.

## Sonarr / Radarr download client settings

**Leave the Category field empty.** Sonarr calls it Category, Radarr calls it Category;
both must be blank.

If you set it, the \*arr app **appends it to the download directory reported by
`session-get`** when working out where this client puts things:

```csharp
if (Settings.MovieCategory.IsNotNullOrWhiteSpace())
{
    destDir = $"{destDir}/{Settings.MovieCategory}";
}
```

So with `DOWNLOAD_DIR=/data/Downloads/itv` and a category of `itv`, the app looks for
`/data/Downloads/itv/itv`, which does not exist, and reports:

```
You are using docker; download client Transmission places downloads in
/data/Downloads/itv/itv but this directory does not appear to exist inside
the container. Review your remote path mappings and container volume settings.
```

**The category does not select the Put.io folder.** `TARGET_LABEL` does that. The label
on an incoming `torrent-add` is parsed and discarded — every transfer is filed under
`TARGET_LABEL` regardless of what the requesting app asked for. Clearing the field
therefore cannot move anything on Put.io.

Nor does clearing it lose any filtering. The \*arr apps keep a torrent when its path
contains the category as a segment, and `DOWNLOAD_DIR` ends in the label, so every
transfer matched anyway.

> Both apps currently see **every** transfer under `TARGET_LABEL`, not just their own,
> because the per-request category is ignored. That is tracked separately and is worth
> knowing about: dismissing a queue item with "remove from client" deletes the transfer
> from Put.io, including the other app's copy.

## Remote path mapping

`DOWNLOAD_DIR` is advertised **as this container sees it**. Whether your \*arr apps
need a remote path mapping depends entirely on your mounts.

**If both containers mount the volume at the same path** — no mapping needed:

```yaml
seedbox_downloader:
  environment:
    DOWNLOAD_DIR: /downloads
  volumes:
    - media:/downloads

sonarr:
  volumes:
    - media:/downloads        # same path -- nothing to translate
```

**If they mount it at different paths** — add a mapping in the \*arr app translating
`DOWNLOAD_DIR` to whatever that app sees:

```yaml
seedbox_downloader:
  environment:
    DOWNLOAD_DIR: /data/Downloads/itv
  volumes:
    - media:/data/Downloads/itv

sonarr:
  volumes:
    - media:/downloads        # different path -- mapping required
```

In Sonarr or Radarr, go to **Settings → Download Clients → Remote Path Mappings**
and add:

| Field | Value |
|---|---|
| Host | the hostname you configured for this client, e.g. `seedbox_downloader` |
| Remote Path | `/data/Downloads/itv` — the value of `DOWNLOAD_DIR` |
| Local Path | `/downloads` — where the \*arr container sees the same files |

The simplest setup is to mount the volume at the same path everywhere and skip
mappings entirely.

### If you are upgrading

Earlier versions advertised `/<TARGET_LABEL>` — a **Put.io-side** path that did not
exist locally at all. If imports were working for you, it was because a remote path
mapping was translating that phantom path to your real one.

That mapping is now wrong. Either delete it, if your mounts agree, or change its
**Remote Path** from `/<TARGET_LABEL>` to your `DOWNLOAD_DIR`.

`PUTIO_BASE_DIR` has been removed. It only ever fed that phantom path. Setting it now
has no effect and causes no error.

## Recovering content downloaded by an earlier version

Single-file transfers downloaded before this change sit inside a wrapper folder, so
the newly advertised path — the bare file — does not exist and those queue items stay
stuck.

There is no automatic migration. A wrapper folder cannot be reliably told apart from
a release folder that legitimately contains one file, and guessing wrong moves
someone's media.

To flatten them by hand, from inside `DOWNLOAD_DIR`, first review what would move:

```sh
find . -mindepth 2 -maxdepth 2 -type f -exec sh -c '
  [ "$(find "$(dirname "$1")" -maxdepth 1 -type f | wc -l)" -eq 1 ] && echo "$1"
' _ {} \;
```

Then, once you are satisfied the list is right:

```sh
find . -mindepth 2 -maxdepth 2 -type f -exec sh -c '
  d=$(dirname "$1")
  [ "$(find "$d" -maxdepth 1 -type f | wc -l)" -eq 1 ] || exit 0
  mv -n "$1" . && rmdir "$d"
' _ {} \;
```

This moves a file up one level only when it is the sole file in its folder, and removes
the folder only if it is then empty. `mv -n` refuses to overwrite.

Two things to expect, both intentional:

- **A folder containing a sidecar file is skipped.** If a wrapper also holds a `.nfo`,
  `.srt`, or similar, it counts as more than one file and is left alone. Move those by
  hand if you want them flattened.
- **A name clash prints `rmdir: <dir>: Directory not empty`.** That means a file of the
  same name already existed at the top level, so `mv -n` declined and the folder was
  correctly left in place. Nothing was overwritten; resolve those individually.

Re-downloading is the alternative if you would rather not run this.
