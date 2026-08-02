//go:build !windows

package garage

import "syscall"

// processAlive reports whether a pid is still running, and whether the answer
// is trustworthy on this platform. Signal 0 delivers nothing and only asks the
// kernel whether the process is there.
func processAlive(pid int) (alive, known bool) {
	return syscall.Kill(pid, 0) == nil, true
}
