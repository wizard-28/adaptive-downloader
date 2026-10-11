package replay

import (
	"errors"
	"os"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

// readStat returns the process state and parent PID from /proc/<pid>/stat.
func readStat(pid int) (state byte, ppid int, ok bool) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, 0, false
	}
	s := string(data)
	i := strings.LastIndexByte(s, ')')
	if i < 0 || i+2 >= len(s) {
		return 0, 0, false
	}
	fields := strings.Fields(s[i+2:])
	if len(fields) < 2 || len(fields[0]) == 0 {
		return 0, 0, false
	}
	pp, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, false
	}
	return fields[0][0], pp, true
}

// descendantPIDs returns the PIDs of all live, non-zombie processes whose
// parent chain leads back to root. It is Linux /proc-based and returns nil
// when /proc is unavailable.
func descendantPIDs(root int) []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	parent := make(map[int]int)
	zombie := make(map[int]bool)
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		state, pp, ok := readStat(pid)
		if !ok {
			continue
		}
		parent[pid] = pp
		zombie[pid] = state == 'Z'
	}
	var out []int
	for pid := range parent {
		if zombie[pid] {
			continue
		}
		for p, steps := parent[pid], 0; p > 1 && steps < 64; p, steps = parent[p], steps+1 {
			if p == root {
				out = append(out, pid)
				break
			}
		}
	}
	sort.Ints(out)
	return out
}

// processAlive reports whether pid exists and is not a zombie.
func processAlive(pid int) bool {
	if err := syscall.Kill(pid, 0); err != nil {
		return errors.Is(err, syscall.EPERM)
	}
	if state, _, ok := readStat(pid); ok && state == 'Z' {
		return false
	}
	return true
}

// alivePIDs filters pids down to the currently live, non-zombie processes.
func alivePIDs(pids []int) []int {
	var out []int
	for _, pid := range pids {
		if processAlive(pid) {
			out = append(out, pid)
		}
	}
	return out
}

// processGroupAlive reports whether any process remains in the group pgid.
func processGroupAlive(pgid int) bool {
	return syscall.Kill(-pgid, 0) == nil
}
