package hostpath

import "golang.org/x/sys/unix"

// searchOnly opens a directory as a location alone, which needs no permission
// on it, and every *at call takes it as its directory.
const searchOnly = unix.O_PATH
