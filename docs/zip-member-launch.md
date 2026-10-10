# Explicit ZIP-member launches

MiSTerClaw accepts an absolute native Main_MiSTer virtual path, for example:

```json
{"mister":"launch","system":"GBA","path":"/media/fat/games/GBA/Collection.zip/USA/Golden Sun.gba"}
```

The member name must match the archive's central directory **exactly**, including
case and subdirectories. MiSTerClaw never chooses the first member, rewrites an
archive, or extracts ROMs to disk. The original virtual path is preserved in the
MGL along with the selected core, delay, index, setname and format overrides.
For new virtual member paths whose archive prefix is a physical regular file,
the MGL attribute is **absolute**, with no legacy `../../../../..` prefix. This
avoids both relative-path overhead and native Main's additional `HomeDir()`
prefix, so a 1,023-byte accepted path fits its 1,024-byte native buffers exactly.
A 1,024-byte member path is rejected by validation and MGL generation.
Main_MiSTer's native file loader reads the member itself.

MGL generation only classifies the filesystem path; it does not open an archive,
verify a member, or extract data. The full launch validation remains mandatory.
Ordinary files, direct NeoGeo archives and existing files inside `.zip`-named
real directories retain their original relative MGL attributes.

Only a compatible `type="f"` file-injection profile is supported. The member
suffix must belong to the selected system's profile (or its format override).
Known profiles use their media extensions rather than discovery's physical
`.zip` folder extension. Stream loaders and disk/CD/VHD suffixes are rejected,
including a disk suffix incorrectly configured as file injection. Existing
physical files, arcade MRA handling and direct NeoGeo `.zip` ROM sets keep their
previous launch behavior. This change does not alter any core defaults.

## Safety and resource limits

Validation runs before descriptor creation or the server's native launch
callback. Both direct Go launches and explicit-path protocol requests validate
members. A protocol launch revalidates in the native launch implementation;
there is no trusted global validation cache.

- Regular, nonsymlink physical archive, at most **256 MiB**.
- At most **4,096 entries** and **4 MiB central-directory metadata**; local
  header/name/extra metadata also has a **4 MiB aggregate limit**.
- At most **64 MiB expanded per entry** and **512 MiB declared expanded total**.
- Selected member CRC and actual output size are checked by bounded streaming
  to a discard sink; other members are not decompressed. A high compression
  ratio alone is not grounds for rejection, so padded ROMs remain supported.
- Store and Deflate compression only, without encryption or unsupported flags.
- Conventional single-disk ZIP only: ZIP64, self-extracting/prefixed archives,
  trailing data and inconsistent local/central headers or descriptors fail.
- Unsafe absolute/traversal/backslash/control-character paths, nonregular or
  symlink entries, overlapping data, duplicate names and ASCII-case collisions
  fail closed. Nested ZIP member paths are unsupported. Directory members and
  empty selected files cannot be launched.
- Native Main splits at the first case-insensitive `.zip` substring. An earlier
  `.zip` directory or `.zip` substring cannot be normalized away to choose a
  different archive. Missing members beneath an actual `.zip` directory are
  not treated as members of an archive. Existing physical files beneath such a
  directory remain subject to the preexisting ordinary-file behavior.
- Physical archive path at most **255 bytes**, complete virtual path at most
  **1,023 bytes**, and each name component at most **255 bytes**. New ZIP paths
  with `&`, `<`, `>`, or double quotes fail with an explicit error because
  Main's MGL parser does not decode XML entities. Apostrophes are supported.

The central-directory bounds and exact entry layout are checked *before*
Go's `archive/zip` reader allocates per-entry objects. Directory and end-record
bytes are frozen in a bounded, revalidated memory snapshot for those allocations,
so concurrent physical-file changes cannot bypass the metadata limits. A post-read entry-count
check alone is insufficient: Go reads headers past the declared directory size
and compares the reported count modulo 65,536. Local headers and data descriptor
bounds are also checked before the selected member is streamed.

Validation detects inode, size or modification-time changes while it reads.
It cannot guarantee immutability after returning: Main reopens the physical
archive separately. Callers must not replace or mutate media during launch.

## Search and discovery scope

ZIP **member discovery is not implemented** in this change. Supply the explicit
archive/member path and system. Search does not index members, and system/folder
counts still describe the existing physical-file discovery. No additional
network scans or archive decompression are introduced into background discovery.

The existing scanner now excludes bare ZIPs for profiles without direct ZIP
support (including GBA and 32X), even if physical-extension discovery reports
`.zip`. NeoGeo direct ZIP ROM sets remain searchable. Thus searching a GBA ZIP
collection cannot silently launch a bare ZIP as though it were a cartridge.
Search also filters old bare-ZIP entries from previously persisted caches, so
GBA query-based auto-launch remains safe before the next rescan. No cache
migration or generic bare-ZIP member guessing is performed.

## Verification

Hardware-free tests cover native-path MGL preservation, GBA and nested 32X
members, direct-file and NeoGeo behavior, profiles and overrides, case/exact-name
matching, path safety, symlinks, duplicate entries, malformed archives, metadata
and expansion limits, compression/encryption, CRC/size verification, high-ratio
padded ROMs, scanner exclusions and callback gating through the request handler.
No real MiSTer load is performed.

Native behavior reference:
[Main_MiSTer file_io.cpp](https://github.com/MiSTer-devel/Main_MiSTer/blob/master/file_io.cpp),
`FileIsZipped` and `FileOpenEx`.
