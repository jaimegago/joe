package runcmd

import "time"

const (
	// CommandTimeout is the maximum time allowed for a command to execute.
	CommandTimeout = 30 * time.Second

	// MaxOutputSize is the maximum size of command output (100KB).
	MaxOutputSize = 100 * 1024
)
