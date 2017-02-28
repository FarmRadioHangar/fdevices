package log

import "fmt"
import "os"

//Info logs info messages. This will log messages only when mode is debug
func Info(msg string, v ...interface{}) {
	if Verbose() {
		logPrefix("[INFO]", msg, v...)
	}
}

// Error logs error message.
func Error(msg string, v ...interface{}) {
	logPrefix("[ERROR]", msg, v...)
}

func logPrefix(prefix, msg string, v ...interface{}) {
	msg = prefix + msg + "\n"
	fmt.Printf(msg, v...)
}

// Verbose returns true if thie application is running in verbose mode
func Verbose() bool {
	return os.Getenv("FDEVICES_MODE") == "debug"
}

// Divider draws a dashed line to give visual context
func Divider() {
	if Verbose() {
		fmt.Println("------------------------------------------")
	}
}
