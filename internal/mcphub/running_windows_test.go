package mcphub

import "github.com/jg/mo/internal/proctree"

func processRunning(pid int) bool {
	return proctree.Alive(pid)
}
