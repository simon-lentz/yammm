// Package location identifies a source and a place in it: [SourceID], the
// identity of a source; [Position], a point in its text; and [Span], a range of
// its text. Diagnostics, schema records and the language server carry them.
//
// Besides the standard library it depends only on internal/hostpath and
// golang.org/x/text/unicode/norm (see Dependencies), so every package but
// internal/hostpath can import it without an import cycle.
//
// # File-backed identities
//
// A file-backed [SourceID] that [SourceIDFromPath] or [ResolveSourcePath] makes
// is the answer to one question — what does the filesystem call this file —
// written in NFC with forward slashes. Its text is:
//   - Absolute (not relative)
//   - Clean (no . or .. segments)
//   - NFC-normalized (Unicode)
//   - Written with "/" as its separator, on every host
//   - Resolved as far as the process can look: symlinks followed, and each
//     component it can look up spelled as the host's answer spells it; a
//     missing tail and anything under a component it cannot traverse are kept
//     as typed, and a descriptor path, a path whose links reach one and a path
//     the host spells inside a descriptor table are named as "Descriptor
//     paths" states (see it and the section on ".." below)
//   - Valid UTF-8
//
// Resolution follows the host's own path rules. On Windows a backslash is a
// separator and is written as "/", and a network share resolves like any other
// path. A drive resolves to what it stands for: a SUBST drive to the directory
// it substitutes, and a mapped drive to its share, so a file reached through a
// share of a local directory keeps the share's identity beside its local one.
// On Unix a backslash is a file-name character and is kept, and a leading "//"
// names the root, as the kernel resolves it.
//
// An identity is not a path to open. NFC can change its bytes, so on a
// filesystem that distinguishes normalization forms it may name no file; read a
// file through the path as given or the host path [ResolveSourcePath] returns
// beside the identity.
//
// Two spellings that resolve to one host path give one identity: the spelling
// the filesystem holds, written in NFC, with a missing tail as typed; a
// descriptor path and a path whose links reach one are named as "Descriptor paths"
// below states. A file that two host paths reach — a hard link, a firmlink or
// a bind mount — keeps two identities. [SourceID.Rel] compares two identities segment by segment, never through the
// host.
//
// # A ".." is evaluated on the text
//
// Case, the on-disk normalization and every symbolic link are read from disk, as
// far as the path exists, the process can look and the path does not reach a
// descriptor's file. A ".." is not. It is evaluated
// on the path's text before anything is looked up, so "dir/link/../x" names
// "dir/x" whatever "dir/link" resolves to. A "." and a repeated separator are
// cleaned on the text in the same step. A path that ends in a separator or a
// "." element names a directory, as a Unix kernel reads it, on every host: one whose last element
// exists and is not a directory is refused with an [fs.PathError] whose error
// matches syscall.ENOTDIR under errors.Is, is not itself a syscall.Errno, and
// reads "not a directory" on every host, and the identity of one that is a directory, or does not exist yet, is
// written without the separator. The refusal never matches [fs.ErrNotExist],
// though Windows' own ENOTDIR, ERROR_PATH_NOT_FOUND, does. Windows answers a
// path under a regular file as missing, so there the first element above it
// that exists is judged, and a file there is refused the same way: the
// resolver, a schema read and an import read each refuse it on every host.
//
// This is a decision, not an oversight, and for a ".." the path itself holds it
// is the same decision on every host. A ".." inside a link's target is another
// matter, stated below. Microsoft's path documentation states that Windows
// normalizes a path it is handed, unless the path starts exactly with \\?\, and
// removes each ".." with the segment before it, so the text rule IS the host rule
// there. A POSIX
// kernel instead takes the parent of the directory it actually reached, so on
// Unix this package and the shell can name two different files for one path.
//
// The reason is agreement. The schema loader resolves a "../" import through
// filepath.Join, on the text; an editor resolves the file it is editing through
// this package; a data file takes its identity from it too, and the command-line tool
// evaluates a ".." the same way in every path it reads or writes. One rule for
// all of them lands them on one file, and a resolver that followed the kernel
// here would send the editor to a file the loader does not compile.
// The resolver's tests in internal/hostpath hold the rule.
//
// One consequence follows, and it is intended: the text decides before the disk
// is read, whatever the component is. A ".." after a symbolic link does not follow
// it. A ".." after a component that does not exist, a regular file or a directory
// the process cannot enter cancels that component where the kernel would refuse
// the path. Only a ".." parts the text rule from the kernel.
//
// For a path "Descriptor paths" below does not name, the answer on
// darwin is realpath(3), called through libSystem. On APFS it reads each
// component's name from the directory the lookup passed through, so it answers
// in the volume's case and normalization; it opens nothing, so a FIFO, an
// unreadable file and a full descriptor table resolve like any other path.
// fcntl(F_GETPATH) is not used: it answers from a vnode's cached name, which a
// concurrent lookup of another hard link of the same file rewrites. On a FAT
// volume realpath answers in the case of the lookup that made the kernel's
// current record of the file, which the kernel can discard while the volume
// stays mounted, so a later answer can take another case (measured on FAT32;
// exFAT is not measured). A firmlink is not a symbolic link, so
// /System/Volumes/Data/Users and /Users stay two spellings, as a bind mount
// does on Linux. On Windows the answer is the final path of the file
// (GetFinalPathNameByHandleW), read from a handle opened with no access. That is
// the path fully resolved, so a path through a junction gives the junction
// target's identity. An SMB component the user may not query
// falls back to the name as opened, which is not normalized. filepath.EvalSymlinks is not used
// there: it follows a link named inside a target before a ".." that follows
// it, where Windows evaluates the ".." first. The path is made absolute once,
// which trims an element's trailing dots and spaces, and every later lookup is
// made in extended form (\\?\), so none trims them again whatever the path's
// length. On Linux and every other Unix host but darwin, filepath.EvalSymlinks
// is the answer: it keeps the typed case, so on a case-folding filesystem, or a
// bind mount of one, each spelling of a file has its own identity.
//
// A path that does not exist yet resolves as far as it exists: the deepest
// existing ancestor is spelled on disk and the missing tail is appended as
// typed, and a dangling symbolic link resolves to the path the kernel will
// reach through it. On Unix each ".." in its target takes the parent of the
// directory reached so far on disk, not a parent read from the target's text.
// On Windows the target replaces the link in the path's text and the text's
// ".." is evaluated before any further link is followed. That text starts from
// the link's directory as spelled on disk, so a link reached through a junction
// reads its target from the junction's target. Such a link's identity settles
// once its target exists. An
// identity minted for a file before it is written therefore equals the one
// minted after, which is what lets an in-memory source key match the file it
// will become. A component the process cannot traverse ends the resolution the
// same way. A path under a regular file can never exist and is refused. On
// Unix a dangling link whose target holds a ".." has the part of its target up
// to the last ".." spelled by the host's speller, which takes each ".." on disk
// as the kernel does, and a target the speller refuses, one that takes a ".."
// past a component that does not exist or is not a directory, or out of a
// directory whose resolved form is too long, is refused with the speller's
// error. So is a cycle of dangling links, and so is an existing path whose
// resolution passes through a resolved form longer than the host allows a path
// to be (PATH_MAX, 1024 bytes on darwin), though its own resolved form may be
// shorter and the kernel may open it.
//
// # Descriptor paths
//
// On every host but Windows, a descriptor path names an open descriptor, not a
// file. It is a path whose directory is /dev, by whatever route the path
// reaches it, and whose name is stdin, stdout or stderr; one whose directory is
// /dev/fd and whose name is a number; or, on Linux, an entry of a procfs
// descriptor table. Its file may be a pipe or an unlinked file with no name, and
// on darwin the host gives no true name for a regular one. So a descriptor path
// of a file that is not a directory resolves to its name written from the
// directory that judged it, /dev/ or /dev/fd/ and its number in decimal with no
// leading zero, so /dev/stdin and /DEV/stdin on a case-insensitive volume give
// one identity, and on Linux so do /dev/fd/3, /proc/self/fd/3 and the process's
// /proc/<pid>/fd/3; an entry of another procfs table, as another process's or a
// thread's, keeps its spelling, and on Linux a
// number with a leading zero names no entry. A path whose links end at such a
// descriptor path, open or not, keeps its typed last element, as does a path
// the host spells inside a descriptor table, as darwin spells a file under a
// directory's descriptor: the host's name for it may reach no file or another
// file. The directory that holds that element is spelled as any directory is. A
// descriptor of a directory reaches a directory with a name, and is spelled as
// any directory is: Linux names it through procfs, and darwin's answer lies in
// the descriptor table, so there it keeps its typed last element in /dev/fd,
// and /dev/fd/04 and /dev/fd/4 stay two identities.
//
// # SourceID
//
// SourceID identifies a source within a build. A file-backed one is compared up
// to NFC: on a host that keeps both forms of a name, two files named in NFC and
// NFD take one SourceID. It supports two modes:
//   - File-backed: Created via SourceIDFromPath or ResolveSourcePath.
//   - Synthetic: Created via NewSourceID for non-file sources
//     like "<stdin>", "inline:test", or "test://unit/person.yammm". NewSourceID
//     refuses a name that could not be told apart from another identity: an
//     empty one, one that is not valid UTF-8 or holds a character that does not
//     print, and one that starts as an absolute path does on some host: with a
//     slash, two backslashes, or an ASCII letter, a colon and a separator.
//
// SourceID is comparable and safe for use as map keys. Case is not folded, it
// is read from the filesystem as far as the path resolves (see above): where
// /Users/Simon/a.yammm and /users/simon/a.yammm name one file, both give the
// identity of the spelling the volume holds. A synthetic identifier is compared
// as written.
//
// # Position
//
// Position identifies a point in a UTF-8 encoded source file, in
// [go/token.Position]'s model:
//   - Line: 1-based line number
//   - Column: 1-based column counting Unicode code points (runes), not bytes
//   - Offset: 0-based byte offset
//
// The zero Position is unknown, and [Position.IsValid] reports a known one: a
// Line above zero. Every producer in this module sets all three fields, so a
// valid position's Offset is known, and an Offset of 0 is the first byte.
//
// # Span
//
// Span represents a half-open range [Start, End) in a source file:
//   - Source: SourceID identifying the source
//   - Start: Inclusive start position
//   - End: Exclusive end position (equals Start for point spans)
//
// A span is a struct literal; nothing checks that Start is not after End. The
// zero Span is "no location", which [Span.IsZero] reports. [Span.Compare]
// orders spans as [time.Time.Compare] orders times, so
// slices.SortFunc(spans, location.Span.Compare) sorts them.
//
// # Dependencies
//
//	location  ──imports──▶  internal/hostpath, golang.org/x/text/unicode/norm
//
// internal/hostpath resolves a host path, and the x/text import is NFC
// normalization.
package location
