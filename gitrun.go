package main

// Running git in the current directory.
//
// This file is the same in every tool of the family that needs it.

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
)

func runGit(ctx context.Context, args ...string) (string, error) {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", errors.New(msg)
		}
		return "", err
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func insideWorkTree() bool {
	out, err := runGit(context.Background(), "rev-parse", "--is-inside-work-tree")
	return err == nil && out == "true"
}
