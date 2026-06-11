package logger

import (
	"fmt"
	"os"
	"sync"
	"time"
)

var (
	logFileMutex sync.Mutex
	logFile      *os.File
)

func init() {
	os.MkdirAll("outbound-data", 0755)
	f, err := os.OpenFile("outbound-data/cli.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		logFile = f
	}
}

func writeLog(msg string) {
	fmt.Print(msg)
	if logFile != nil {
		logFileMutex.Lock()
		logFile.WriteString(msg)
		logFileMutex.Unlock()
	}
}

func Info(format string, v ...interface{}) {
	writeLog(fmt.Sprintf("[%s] [INFO] %s\n", time.Now().Format(time.RFC3339), fmt.Sprintf(format, v...)))
}

func Debug(format string, v ...interface{}) {
	writeLog(fmt.Sprintf("[%s] [DEBUG] %s\n", time.Now().Format(time.RFC3339), fmt.Sprintf(format, v...)))
}

func Warn(format string, v ...interface{}) {
	writeLog(fmt.Sprintf("[%s] [WARN] %s\n", time.Now().Format(time.RFC3339), fmt.Sprintf(format, v...)))
}

func Error(format string, v ...interface{}) {
	writeLog(fmt.Sprintf("[%s] [ERROR] %s\n", time.Now().Format(time.RFC3339), fmt.Sprintf(format, v...)))
}

func System(format string, v ...interface{}) {
	writeLog(fmt.Sprintf("[%s] [SYSTEM] %s\n", time.Now().Format(time.RFC3339), fmt.Sprintf(format, v...)))
}
