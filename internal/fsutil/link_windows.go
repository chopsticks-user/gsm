//go:build windows

package fsutil

import (
	"fmt"
	"os/exec"
)

// makeDirLink creates a directory junction. Unlike symlinks, junctions need
// neither admin rights nor Developer Mode.
func makeDirLink(target, link string) error {
	out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		return fmt.Errorf("mklink: %v: %s", err, out)
	}
	return nil
}
