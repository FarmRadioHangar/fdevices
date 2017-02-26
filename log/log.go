package log

import "fmt"

//Info logs info messages
func Info(msg string, v ...interface{}) {
	msg = "[INFO]" + msg + "\n"
	fmt.Printf(msg, v...)
}

// Error logs error message
func Error(msg string, v ...interface{}) {
	msg = "[INFO]" + msg + "\n"
	fmt.Printf(msg, v...)
}
