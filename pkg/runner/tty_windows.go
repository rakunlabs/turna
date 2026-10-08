package runner

import (
	"fmt"
	"os"
)

func startTTY(_ string, _ []string, _ *os.ProcAttr) (*os.Process, func(), error) {
	return nil, nil, fmt.Errorf("tty services are not supported on Windows")
}
