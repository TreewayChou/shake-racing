//go:build !linux

package tunnel

import "os/exec"

func setProcAttr(*exec.Cmd) {}
