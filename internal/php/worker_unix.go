//go:build !windows

package php

import "os/exec"

func configureWorkerCommand(_ *exec.Cmd) {}
