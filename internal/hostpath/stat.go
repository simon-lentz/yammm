package hostpath

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"runtime"
	"syscall"
)

// Lookups is what [ClassifyIn] asks of a filesystem: the host's own, or an
// [os.Root]'s, which answers a name relative to its root and refuses one that
// leaves it.
type Lookups interface {
	Stat(name string) (fs.FileInfo, error)
	Lstat(name string) (fs.FileInfo, error)
	Readlink(name string) (string, error)
}

// hostLookups is the host's filesystem. With extended set, an absolute clean
// path is looked up in its extended form on Windows ([lookupName]).
type hostLookups struct{ extended bool }

func (h hostLookups) name(p string) string {
	if h.extended {
		return lookupName(p)
	}
	return p
}

// The host's own path errors are what [Lookups] answers, as [os.Root]'s are.
func (h hostLookups) Stat(p string) (fs.FileInfo, error)  { return os.Stat(h.name(p)) }     //nolint:wrapcheck // the host's path error
func (h hostLookups) Lstat(p string) (fs.FileInfo, error) { return os.Lstat(h.name(p)) }    //nolint:wrapcheck // the host's path error
func (h hostLookups) Readlink(p string) (string, error)   { return os.Readlink(h.name(p)) } //nolint:wrapcheck // the host's path error

// Stat is the host's answer for p, a path [Text] returned: p's element when the
// host reaches one, [ErrNotDirectory] when p names a directory ([NamesDirectory])
// and its element is not one, or sits under a file, and otherwise the error the
// host gave, which [Classify] has judged. Windows drops a directory-form ending,
// so there the element is stat'ed without it and judged as a Unix kernel judges
// it.
func Stat(p string) (fs.FileInfo, error) {
	return statIn(hostLookups{}, p)
}

func statIn(l Lookups, p string) (fs.FileInfo, error) {
	dirForm := NamesDirectory(p)
	name := p
	if dirForm && runtime.GOOS == "windows" {
		name = directoryElement(p)
	}
	info, err := l.Stat(name)
	switch {
	case err != nil:
		return nil, ClassifyIn(l, p, err)
	case dirForm && !info.IsDir():
		return nil, ErrNotDirectory
	}
	return info, nil
}

// Classify returns the error a stat or open of p reports when the host answered
// it with err: [ErrNotDirectory] when an element p needs as a directory exists
// and is not one, which a Unix kernel answers ENOTDIR and Windows answers as a
// missing path, and err itself otherwise.
func Classify(p string, err error) error {
	return ClassifyIn(hostLookups{}, p, err)
}

// ClassifyIn is [Classify] over l. A missing p is judged by the first element
// above it that exists, each dangling link on the way followed to the path the
// kernel reaches through it: a file there is [ErrNotDirectory], and a directory
// leaves err standing. A lookup answered ENOTDIR is [ErrNotDirectory]; one that
// fails otherwise, other than as missing, ends the climb with err, as does a
// chain of more than maxLinkHops links.
func ClassifyIn(l Lookups, p string, err error) error {
	switch {
	case unixNotDirectory(err):
		return ErrNotDirectory
	case !errors.Is(err, fs.ErrNotExist):
		return err
	}
	element := directoryElement(p)
	below := NamesDirectory(p) // whether the kernel needs element as a directory
	for hops := 0; ; {
		info, serr := l.Stat(element)
		switch {
		case serr == nil && info.IsDir():
			return err
		case serr == nil:
			if below {
				return ErrNotDirectory
			}
			return err
		case unixNotDirectory(serr):
			return ErrNotDirectory
		case !errors.Is(serr, fs.ErrNotExist):
			return err
		}
		if next, ok := danglingHop(l, element); ok {
			if hops++; hops > maxLinkHops {
				return err
			}
			element = directoryElement(next)
			continue
		}
		up := Parent(element)
		if up == element {
			return err
		}
		element, below = up, true
	}
}

// danglingHop returns the path the kernel reaches through the link at element,
// when element is a link whose target [readLinkIn] reads.
func danglingHop(l Lookups, element string) (string, bool) {
	info, err := l.Lstat(element)
	if err != nil || info.Mode()&fs.ModeSymlink == 0 {
		return "", false
	}
	dest, err := readLinkIn(l, element)
	if err != nil {
		return "", false
	}
	return followLink(element, dest), true
}

// OpenFile opens p, a path [Text] returned, for reading with flag, and judges a
// failure as [Classify] does. A path that names a directory is refused when its
// element is not one, on every host: Windows would open the file a
// directory-form ending names, so there [Stat] judges p first, and a Unix kernel
// answers ENOTDIR to the open itself. An error is an [fs.PathError] naming p.
func OpenFile(p string, flag int) (*os.File, error) {
	if runtime.GOOS == "windows" && NamesDirectory(p) {
		if _, err := Stat(p); err != nil {
			return nil, &fs.PathError{Op: "open", Path: p, Err: bareErr(err)}
		}
	}
	f, err := os.OpenFile(p, flag, 0)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: p, Err: bareErr(Classify(p, err))}
	}
	return f, nil
}

// Hop returns the path the kernel reaches through the symbolic link at link:
// its target, read as [readLinkIn] reads it, from the link's own directory and
// uncleaned. Every walk that follows links by hand takes each hop through it.
func Hop(link string) (string, error) {
	dest, err := readLinkIn(hostLookups{}, link)
	if err != nil {
		return "", err
	}
	return followLink(link, dest), nil
}

// readLinkIn reads the target of the link at name. An empty target reaches no
// file, as darwin's kernel answers it, so it is read as ENOENT on every host.
func readLinkIn(l Lookups, name string) (string, error) {
	dest, err := l.Readlink(name)
	switch {
	case err != nil:
		return "", err //nolint:wrapcheck // the lookup's own error names the link
	case dest == "":
		return "", &fs.PathError{Op: "readlink", Path: name, Err: syscall.ENOENT}
	}
	return dest, nil
}

// ErrNotRegularFile is what [OpenRegular] returns for a path that names
// anything but a regular file.
var ErrNotRegularFile = errors.New("not a regular file")

// OpenRegular opens a file for reading through open and refuses anything but a
// regular file. The open is non-blocking so a FIFO cannot stall it, and the
// kind is read from the open descriptor so nothing can change between check and
// read. stat classifies a failed open, which a socket produces: there is no
// descriptor to read the kind from, and nothing is opened afterwards.
func OpenRegular(open func(flag int) (*os.File, error), stat func() (fs.FileInfo, error)) (*os.File, error) {
	f, err := open(os.O_RDONLY | syscall.O_NONBLOCK)
	if err != nil {
		if info, serr := stat(); serr == nil && !info.Mode().IsRegular() {
			return nil, ErrNotRegularFile
		}
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close() //nolint:gosec // the stat's failure is the one reported
		return nil, fmt.Errorf("stat: %w", err)
	}
	if !info.Mode().IsRegular() {
		f.Close() //nolint:gosec // opened only to read its kind
		return nil, ErrNotRegularFile
	}
	return f, nil
}
