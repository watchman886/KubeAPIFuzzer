package main

import (
	"fmt"
	"k-fuzz/cmd"
	"runtime"

	"github.com/sirupsen/logrus"
)

func main() {
	logrus.SetLevel(logrus.DebugLevel)
	logrus.SetReportCaller(true)

	logrus.SetFormatter(&logrus.TextFormatter{
		// add a space between the log level and the message
		CallerPrettyfier: func(frame *runtime.Frame) (function string, file string) {
			return frame.Function, fmt.Sprintf(" %s:%d", frame.File, frame.Line)
		},
	})

	cmd.Execute()
}
