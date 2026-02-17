package cmd

import (
	"os"
)

// ANSI color codes for terminal output. Colors are automatically disabled
// when stdout is not a terminal or the NO_COLOR env var is set.
// See: https://no-color.org/

var colorEnabled = detectColor()

func detectColor() bool {
	// Respect the NO_COLOR standard.
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func green(s string) string {
	if !colorEnabled {
		return s
	}
	return "\033[32m" + s + "\033[0m"
}

func yellow(s string) string {
	if !colorEnabled {
		return s
	}
	return "\033[33m" + s + "\033[0m"
}

func bold(s string) string {
	if !colorEnabled {
		return s
	}
	return "\033[1m" + s + "\033[0m"
}
