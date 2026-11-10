package proc

import (
	"errors"
	"os"
	"strconv"
	"strings"
)

// StartTime returns the process start time in clock ticks since boot, from
// /proc/<pid>/stat. A zombie counts as gone.
func StartTime(pid int) (string, error) {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return "", err
	}
	// The command name (field 2) is in parentheses and may contain spaces,
	// so split after the last ')'. Field 3 is the state, field 22 the start time.
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return "", errors.New("malformed /proc stat")
	}
	f := strings.Fields(s[i+1:])
	if len(f) < 20 {
		return "", errors.New("malformed /proc stat")
	}
	if f[0] == "Z" || f[0] == "X" {
		return "", errors.New("process exited")
	}
	return f[19], nil
}
