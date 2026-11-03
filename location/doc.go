// Package location provides source location tracking for diagnostics.
//
// This package defines the core types used by the YAMMM diagnostic system
// to track source locations. Besides the standard library it depends only on
// its location/path sub-package, internal/hostpath, golang.org/x/text/unicode/norm
// and, on Windows, golang.org/x/sys/windows (see Dependencies), so every package
// but location/path and internal/hostpath can import it without an import cycle.
//
// # CanonicalPath
//
// CanonicalPath represents a canonicalized file system path that is always:
//   - Absolute (not relative)
//   - Clean (no . or .. segments)
//   - NFC-normalized (Unicode)
//   - Written with "/" as its separator, on every host
//   - Resolved as far as the process can look: symlinks followed, and each
//     component it can look up spelled as the filesystem spells it; a missing
//     tail, anything under a component it cannot traverse, and a descriptor
//     path, is kept as typed (see the section on ".." below)
//   - Valid UTF-8
//
// Canonicalization follows the host's own path rules. On Windows a backslash
// is a separator and is written as "/", and a network share is canonicalized
// like any other path; on Unix a backslash is a file-name character and is
// kept, and a leading "//" names the root, as the kernel resolves it.
//
// A CanonicalPath is an identity, not a path to open. NFC can change its bytes,
// so on a filesystem that distinguishes normalization forms it may name no
// file; read a file through the path as given or the host path it was derived
// from, and through the path as given when a link may reach a descriptor (see
// below).
//
// Create via NewCanonicalPath. The type uses an unexported
// field to enforce construction through validated constructors only.
//
// # Host paths and identities
//
// [ResolveHostPath] answers one question — what does the filesystem call this
// file — and a file-backed identity is that answer in NFC with forward slashes,
// save as this section states. A descriptor
// path names a descriptor, not a file, and answers as typed, case included (see
// below). The constructors [SourceIDFromPath], [ResolveSourcePath],
// [NewCanonicalPath] and [CanonicalPath.Join] all resolve; [CanonicalPath.Dir]
// cuts an identity's text, so a descriptor path's directory keeps its typed
// spelling too. Two
// spellings that resolve to one host path therefore give one identity: the
// spelling the filesystem holds, written in NFC, with a missing tail, or a
// descriptor path, as typed. A file that two host paths reach — a
// hard link, a firmlink or a bind mount — keeps two identities.
//
// # A ".." is evaluated on the text
//
// Case, the on-disk normalization and every symbolic link are read from disk, as
// far as the path exists, the process can look and the path is not a descriptor
// path. A ".." is not. It is evaluated
// on the path's text before anything is looked up, so "dir/link/../x" names
// "dir/x" whatever "dir/link" resolves to. A "." and a repeated or trailing
// separator are cleaned on the text in the same step.
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
// [TestResolveHostPath_KeepsWhatTheLSPDependsOn] holds the rule.
//
// One consequence follows, and it is intended: the text decides before the disk
// is read, whatever the component is. A ".." after a symbolic link does not follow
// it. A ".." after a component that does not exist, a regular file or a directory
// the process cannot enter cancels that component where the kernel would refuse
// the path, and a "." or a trailing separator after a regular file is dropped
// where the kernel would refuse it too.
//
// For a path that is not a descriptor path, the answer on darwin is realpath(3),
// called through libSystem. It reads each
// component's name from the directory the lookup passed through, so it answers
// in the volume's case and normalization; it opens nothing, so a FIFO, an
// unreadable file and a full descriptor table resolve like any other path.
// fcntl(F_GETPATH) is not used: it answers from a vnode's cached name, which a
// concurrent lookup of another hard link of the same file rewrites. A firmlink is not a symbolic link, so
// /System/Volumes/Data/Users and /Users stay two spellings, as a bind mount
// does on Linux. On Windows the answer is the final path of the file
// (GetFinalPathNameByHandleW), read from a handle opened with no access. That is
// the path fully resolved, so a path through a junction gives the junction
// target's identity. An SMB component the user may not query
// falls back to the name as opened, which is not normalized. filepath.EvalSymlinks is not used
// there: it follows a link named inside a target before a ".." that follows
// it, where Windows evaluates the ".." first. On Linux filepath.EvalSymlinks
// is the answer for such a path: the host is case-sensitive, and a directory mounted with case
// folding (ext4 +F) keeps the typed case and is out of scope.
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
// same way. A path under a regular file can never exist and is refused, as is,
// on Unix, a dangling link whose target takes a ".." past a component that
// does not exist or is not a directory, since the kernel reaches no file through it; so is a cycle of dangling links, and so is an existing path whose resolved form is
// longer than the host allows a path to be (PATH_MAX, 1024 bytes on darwin).
//
// On every host but Windows, a descriptor path resolves to itself. It is a path
// whose directory is /dev, by whatever route the path reaches it, and whose name
// is stdin, stdout or stderr; one whose directory is /dev/fd and whose name is a
// number; or, on Linux, an entry of a procfs descriptor table. Such a path
// names an open descriptor, not a file: its file may be a pipe or an unlinked file
// with no name, and on darwin the host gives no true name for a regular one. Two
// spellings of one descriptor path, such as /dev/stdin and /DEV/stdin on a
// case-insensitive volume, give two identities. A link to one resolves as far as
// the host names it, and that name may reach no file or another file, so only
// the path as given is sure to reach the descriptor.
//
// # SourceID
//
// SourceID identifies a source within a build, up to NFC: on a host that keeps
// both forms of a name, two files named in NFC and NFD take one SourceID. It
// supports two modes:
//   - File-backed: Created via SourceIDFromPath or ResolveSourcePath.
//     Stores a CanonicalPath directly.
//   - Synthetic: Created via NewSourceID for non-file sources
//     like "<stdin>", "inline:test", or "test://unit/person.yammm".
//
// SourceID is comparable and safe for use as map keys. Case is not folded, it
// is read from the filesystem as far as the path resolves (see CanonicalPath
// above): where /Users/Simon/a.yammm and
// /users/simon/a.yammm name one file, both give the identity of the spelling
// the volume holds. A synthetic identifier is compared as written.
//
// # Position
//
// Position identifies a point in a UTF-8 encoded source file:
//   - Line: 1-based line number (0 = unknown)
//   - Column: 1-based column counting Unicode code points (runes), not bytes
//   - Byte: 0-based byte offset (-1 = unknown)
//
// Use IsZero() to check for unknown positions, IsKnown() to check for valid
// line/column, and HasByte() to check for known byte offsets.
//
// # Span
//
// Span represents a half-open range [Start, End) in a source file:
//   - Source: SourceID identifying the source
//   - Start: Inclusive start position
//   - End: Exclusive end position (equals Start for point spans)
//
// Create spans via PointWithByte or RangeWithBytes. RangeWithBytes panics
// if end is before start by an order both positions know: the byte offsets, or
// the line and column. A position with
// a zero line or column knows no line and column order, and one with both zero
// knows no byte order ([Position.HasByte]). A span whose
// two orders disagree is unsafe whichever one a reader takes, so RangeWithBytes
// refuses it. A Span struct literal gets no such check.
//
// Use IsZero() to check for "no location" and IsValid() to check for LSP
// compatibility.
//
// # RelatedInfo
//
// RelatedInfo provides supplementary location context for a diagnostic, such as
// the declaration a case-colliding property collides with.
//
// # Provenance
//
// [Provenance] tracks the origin of a parsed data element through the
// adapter → validator → graph pipeline. It carries the source name,
// a [github.com/simon-lentz/yammm/location/path] builder for JSONPath-like
// instance paths, an optional raw path, and an optional [Span] for its source
// location, which need not hold a byte offset.
// Create via [NewProvenance], extending the path through its own
// [github.com/simon-lentz/yammm/location/path.Builder] while parsing.
//
// Every method is safe on a zero Provenance: [Provenance.WithRawPath] turns it
// into a new Provenance carrying that raw path and no source information, and
// [Provenance.SourceName], [Provenance.Path] and [Provenance.Span] return zero
// values. A zero Provenance therefore keeps a diagnostic's path without claiming a
// source location it does not have.
//
// # Dependencies
//
//	location  ──imports──▶  location/path, internal/hostpath, golang.org/x/text/unicode/norm,
//	                        golang.org/x/sys/windows
//
// The sub-package carries provenance paths, internal/hostpath judges a
// descriptor path by its name, the x/text import is NFC normalization, and the
// x/sys import, made on Windows alone, is the final-path call.
package location
