package gatewaybridge

import (
	"os"
	"strconv"
	"strings"
)

func readPID(path string) (int, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- user-supplied or trusted local path; not exposed to untrusted input
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

func readPort(path string) (int, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- user-supplied or trusted local path; not exposed to untrusted input
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

func writePID(path string, pid int) error {
	return os.WriteFile(path, []byte(strconv.Itoa(pid)), 0o600) // #nosec G703 -- internal trusted path
}

func writePort(path string, port int) error {
	return os.WriteFile(path, []byte(strconv.Itoa(port)), 0o600)
}
