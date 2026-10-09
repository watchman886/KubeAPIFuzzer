package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/sirupsen/logrus"
)

// createKeyboardInterruptableContext creates a context that is cancelled when a keyboard interrupt (Ctrl+C) is received
// CAUTIOUS: this function can be only called once per program execution
// CAUTIOUS: after calling this function, defer cancel() should be called in main function to avoid resource leak
func createKeyboardInterruptableContext(parent context.Context) (c context.Context, cancel context.CancelFunc) {

	// create a cancellable context
	c, cancel = context.WithCancel(parent)

	// capture Ctrl+C
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logrus.Debugf("received signal: %s, exiting...", sig)
		cancel()
	}()

	return c, cancel
}

func prepareOutput(outputFile string) (io.Writer, func(), error) {
	if outputFile == "" {
		return os.Stdout, func() {}, nil
	}

	f, err := os.OpenFile(outputFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return nil, func() {}, fmt.Errorf("cannot create output file %s: %w", outputFile, err)
	}

	cleanup := func() {
		err := f.Close()
		if err != nil {
			logrus.Errorf("failed to close output file %s: %v", outputFile, err)
			return
		}
	}
	return f, cleanup, nil
}
