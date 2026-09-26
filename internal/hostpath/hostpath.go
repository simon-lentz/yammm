// Package hostpath holds the module's one rule for turning a path someone typed
// into the path the process hands the kernel.
//
// A ".." the path itself holds is evaluated on the text first, as the schema
// loader and location.ResolveHostPath evaluate it. [Text] applies that step to
// the paths the command-line tool writes and the snapshot and formatter paths
// it reads, and to the path snapshot.WriteFile writes; the tool's data and
// schema paths take the same step through location and filepath.Abs. So each
// reaches the file the loader reads for the same spelling. A POSIX kernel would
// instead take the parent of the directory the component before the ".."
// reached, which differs when that component is a symbolic link; on Windows the
// kernel evaluates a ".." on the text itself, so the two agree there.
//
// Everything after that step is the kernel's. A symbolic link's own target is
// read as the kernel reads it, uncleaned: on Unix each ".." in it takes the
// parent of the directory reached on disk, and on Windows the kernel evaluates
// it on the text. [FollowLink] builds that path, and
// [Parent] the directory that holds a path's last element, without cleaning
// either. [filepath.Clean], [filepath.Dir] and [filepath.Join] would cancel a
// ".." against the component before it, and a caller that stages a file beside
// a target and renames it over the target needs both paths to name one
// directory.
package hostpath

import (
	"os"
	"path/filepath"
	"strings"
)

// Text returns the path the process hands the kernel for p: p cleaned, so a
// ".." cancels the element before it, and made absolute from the working
// directory as [filepath.Abs] spells it when it climbs out of that directory,
// which is where the loader climbs from. Any other relative path stays
// relative, naming the same file in its typed spelling, and the empty path is
// returned unchanged, so it fails where the kernel refuses it.
func Text(p string) (string, error) {
	if p == "" {
		return p, nil
	}
	c := filepath.Clean(p)
	if filepath.IsAbs(c) || (c != ".." && !strings.HasPrefix(c, ".."+string(filepath.Separator))) {
		return c, nil
	}
	return filepath.Abs(c) //nolint:wrapcheck // the caller names the path it was given
}

// Parent returns the directory that holds p's last element, as the kernel
// reaches it: p with that element and the separators before it removed, and
// nothing cleaned. The parent of a relative single element is ".", and the
// parent of a root is the root.
func Parent(p string) string {
	vol := len(filepath.VolumeName(p))
	root := vol
	if root < len(p) && os.IsPathSeparator(p[root]) {
		root++
	}
	i := len(p)
	for i > root && os.IsPathSeparator(p[i-1]) {
		i--
	}
	for i > root && !os.IsPathSeparator(p[i-1]) {
		i--
	}
	j := i
	for j > root && os.IsPathSeparator(p[j-1]) {
		j--
	}
	switch {
	case j > root:
		return p[:j]
	case root > vol:
		return p[:root]
	default:
		// A relative element, or a drive-relative one on Windows.
		return p[:vol] + "."
	}
}

// FollowLink returns the path the kernel reaches through the symbolic link at
// link, whose target is dest. An absolute dest, or one that names a volume, is
// returned as it is. A dest rooted without a volume, on Windows, is read on the
// link's volume. Any other dest is read from the link's own directory, and
// nothing is cleaned, so the kernel evaluates each ".." in it: on Unix from the
// directory reached on disk, on Windows on the text.
func FollowLink(link, dest string) string {
	switch {
	case filepath.IsAbs(dest), filepath.VolumeName(dest) != "":
		return dest
	case dest != "" && os.IsPathSeparator(dest[0]):
		return filepath.VolumeName(link) + dest
	}
	dir := Parent(link)
	if os.IsPathSeparator(dir[len(dir)-1]) {
		return dir + dest
	}
	return dir + string(filepath.Separator) + dest
}
