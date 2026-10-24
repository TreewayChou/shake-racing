//go:build !linux && !windows

package tunnel

import "os/exec"

func setProcAttr(*exec.Cmd) {}

func afterStart(*exec.Cmd) {}
