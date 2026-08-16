# Seedbox Downloader

Pulls completed transfers off a seedbox (put.io or Deluge) onto local disk, and presents
itself to Sonarr/Radarr as a Transmission client so they can import what was pulled.

## Language

### Transfers

**Transfer**:
One unit of work on the seedbox — a torrent the seedbox is fetching or has fetched.
Identified by the seedbox's own id.
_Avoid_: torrent, download (both are overloaded; a Transfer is the seedbox's object,
not ours and not the \*arr app's)

**Transfer Name**:
The name the seedbox gives a Transfer. Cosmetic only — it is **not** authoritative for
any path, because the seedbox may store the content under a different name.
_Avoid_: title, torrent name

**Collision Suffix**:
A short id put.io appends to a name when that name already exists on the account
(e.g. `ojqRfI77`). Present in the File Name and absent from the Transfer Name, which is
why the two must never be treated as interchangeable.

**File Name**:
The name the seedbox actually stored the content under. Authoritative for paths.

**Single-File Transfer**:
A Transfer whose stored content is one file, not a folder. Its content is written
directly into the Local Root with no enclosing folder, matching Transmission.
_Avoid_: single file torrent

**Folder Transfer**:
A Transfer whose stored content is a folder. Its content is written into a folder of
that name inside the Local Root.

### Locations

**Remote Folder**:
Where a Transfer lives *on the seedbox* — a put.io folder or a Deluge save path. Never
leaves the seedbox client; it is meaningless to an \*arr app.
_Avoid_: SavePath, save path, download dir

**Local Root**:
The single directory on local disk into which all Transfers are written.
_Avoid_: download dir (ambiguous — it is also Transmission's word for a per-torrent path)

**Local Layout**:
The pair (Local Root, name) that locates a Transfer's content on local disk. The one
authoritative answer to "where did this land", derived from File Names only. What gets
written to disk and what gets advertised to \*arr are the same Local Layout by
construction, never two independent derivations.

### Lifecycle

**Claim**:
An exclusive hold one instance takes on a Transfer before downloading it, so that two
instances sharing a seedbox account cannot fetch the same content twice.

**Downloaded**:
Every one of a Transfer's files was written with a byte count matching the size the
seedbox reported for it, and its handle closed cleanly. A Transfer that merely finished
copying without erroring is *not* Downloaded — that is how truncated files and error
pages have been mistaken for content.
_Avoid_: complete, finished (the seedbox uses both for its own, unrelated states)

**Imported**:
An \*arr app has taken the content out of the Local Root and into its own library. Ours
to delete only once this has happened.

**Label**:
The name of the Remote Folder used to decide which Transfers on a shared seedbox account
belong to this instance. Transmission calls the same idea a category.
_Avoid_: tag, category
