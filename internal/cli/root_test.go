// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package cli

import (
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestNewRootVersion(t *testing.T) {
	root := NewRoot("dex-client-controller", "test binary")
	var out strings.Builder
	root.SetOut(&out)
	root.SetArgs([]string{"--version"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	got := out.String()
	for _, want := range []string{"dex-client-controller", "dev", "none", "unknown"} {
		if !strings.Contains(got, want) {
			t.Errorf("version output %q missing %q", got, want)
		}
	}
}

func TestExecute(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	tests := []struct {
		name string
		cmd  *cobra.Command
		want int
	}{
		{
			name: "success",
			cmd:  &cobra.Command{Use: "ok", RunE: func(*cobra.Command, []string) error { return nil }},
			want: 0,
		},
		{
			name: "failure",
			cmd: &cobra.Command{
				Use:           "fail",
				SilenceErrors: true,
				RunE:          func(*cobra.Command, []string) error { return errors.New("boom") },
			},
			want: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Execute(tt.cmd, log); got != tt.want {
				t.Errorf("Execute() = %d, want %d", got, tt.want)
			}
		})
	}
}
