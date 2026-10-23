package tunnel

import (
	"os/exec"
	"syscall"
)

// setProcAttr has the kernel kill cloudflared if this process dies without
// a chance to call Stop, so no orphan tunnel is left behind.
func setProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}
