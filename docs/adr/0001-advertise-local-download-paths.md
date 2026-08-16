---
status: accepted
---

# Advertise local download paths, derived from stored file names

The Transmission RPC used to advertise a path built from the seedbox-side folder and
the transfer's name, while content was written to a path built from the stored file
names. Those agree only for a multi-file transfer the seedbox didn't rename, so most
imports silently never happened. We now derive **one** name from the stored file
names and use it for both writing and advertising, and we advertise `DOWNLOAD_DIR`
itself as the download directory — the only path that exists from an \*arr app's
point of view.

## Considered Options

**Persist the resolved destination in the ledger and echo it back.** This was the
obvious fix and it is what the bug report suggested. Rejected as the primary
mechanism: the \*arr apps poll for transfers that are still in progress, when nothing
has been written and there is nothing to read back, so a fallback would be needed
regardless. More importantly it makes divergence *detectable* rather than
*impossible* — the two derivations would still exist, and could still drift. A single
derivation removes the failure mode instead of observing it.

**Keep the wrapper folder around single-file transfers and just advertise it.** This
also works — both layouts import, because Sonarr and Radarr each try the advertised
path as a directory and then as a file. Rejected for two reasons: no other torrent
client invents a folder for a single-file torrent, so it violates the contract we are
imitating; and the import-cleanup step deletes the file it imported, which under a
wrapper layout leaves an empty directory behind permanently.

## Consequences

**This is a breaking change for existing deployments.** Anyone whose imports were
working had a remote path mapping translating the old phantom `/<TARGET_LABEL>` path
to their real one. That mapping is now wrong and must be deleted or repointed at
`DOWNLOAD_DIR`. See `docs/PATHS.md`.

**Content already downloaded by an earlier version will not import.** Single-file
transfers sit inside a wrapper folder that is no longer advertised. There is no
automatic migration: a wrapper folder cannot be reliably distinguished from a release
folder that legitimately holds one file, and guessing wrong moves someone's media.
`docs/PATHS.md` documents a manual flatten.

**`PUTIO_BASE_DIR` is removed.** Its only use was feeding that phantom path. It was
documented as required while having no other effect.

**Remote path mapping is still legitimate** — it remains the correct mechanism when
an \*arr container mounts the download volume at a different path than this service
does. The change makes the advertised path honest, not the mapping unnecessary.
