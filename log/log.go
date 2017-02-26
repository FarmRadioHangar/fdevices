package log

import "fmt"
import "os"

//Info logs info messages. This will log messages only when mode is debug
func Info(msg string, v ...interface{}) {
	if verbose() {
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

func verbose() bool {
	return os.Getenv("FDEVICES_MODE") == "debug"
}
