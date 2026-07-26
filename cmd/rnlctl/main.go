package main

import (
	"context"
	"os"
	"syscall"

	"github.com/luxiaba/remnanode-lite/internal/rnlctl"
)

func main() {
	ctx, stopSignals := rnlctl.NotifySignals(
		context.Background(),
		os.Interrupt,
		syscall.SIGHUP,
		syscall.SIGTERM,
		syscall.SIGQUIT,
	)
	application := rnlctl.New(rnlctl.Options{})
	exitCode := application.Run(ctx, os.Args[1:])
	stopSignals()
	exitCode = rnlctl.SignalExitCode(ctx, exitCode)
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}
