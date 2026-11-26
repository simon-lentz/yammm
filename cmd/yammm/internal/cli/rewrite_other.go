//go:build !unix

package cli

import "io/fs"

// descriptorInfo reports false: off Unix no path names a descriptor of the
// process ([hostpath.DescriptorNumber]).
func descriptorInfo(int) (fs.FileInfo, bool) { return nil, false }
