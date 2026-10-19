package app

import (
	"context"
	"os/exec"
	"strconv"
	"syscall"
)

const createNoWindow = 0x08000000

func toolCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}

	// Cancel the whole process tree, including FFmpeg started by yt-dlp.
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}

		killer := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
		killer.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		return killer.Run()
	}
	return cmd
}
