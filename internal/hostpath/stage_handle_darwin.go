package hostpath

import "golang.org/x/sys/unix"

// searchOnly opens a directory for search alone, which needs no read permission.
const searchOnly = unix.O_SEARCH
