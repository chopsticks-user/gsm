//go:build unix

package fsutil

import "os"

func makeDirLink(target, link string) error { return os.Symlink(target, link) }
