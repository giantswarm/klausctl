package cmd

import "testing"

func TestServeCommandRegistered(t *testing.T) {
	assertCommandOnRoot(t, "serve")
}
