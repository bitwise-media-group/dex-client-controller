// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package cli

import (
	"log/slog"
	"slices"
	"testing"

	"github.com/spf13/cobra"
)

func newBound(t *testing.T) (*Options, *cobra.Command) {
	t.Helper()
	o := &Options{LogLevel: new(slog.LevelVar)}
	cmd := &cobra.Command{Use: "test", RunE: func(*cobra.Command, []string) error { return nil }}
	o.Bind(cmd)
	cmd.Flags().String("dex-address", "", "")
	cmd.Flags().String("watch-namespaces", "", "")
	return o, cmd
}

func load(t *testing.T, args ...string) *Options {
	t.Helper()
	o, cmd := newBound(t)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if err := o.Load(cmd); err != nil {
		t.Fatalf("Load: %v", err)
	}
	return o
}

func TestPrecedence(t *testing.T) {
	t.Run("env beats default", func(t *testing.T) {
		t.Setenv("DEX_CLIENT_CONTROLLER_DEX_ADDRESS", "dex:5557")
		if got := load(t).String("dex-address"); got != "dex:5557" {
			t.Errorf("dex-address = %q, want dex:5557 from env", got)
		}
	})
	t.Run("flag beats env", func(t *testing.T) {
		t.Setenv("DEX_CLIENT_CONTROLLER_DEX_ADDRESS", "dex:5557")
		if got := load(t, "--dex-address", "other:1").String("dex-address"); got != "other:1" {
			t.Errorf("dex-address = %q, want flag value other:1", got)
		}
	})
}

func TestList(t *testing.T) {
	t.Setenv("DEX_CLIENT_CONTROLLER_WATCH_NAMESPACES", " a, b ,,c ")
	if got, want := load(t).List("watch-namespaces"), []string{"a", "b", "c"}; !slices.Equal(got, want) {
		t.Errorf("List = %q, want %q", got, want)
	}
	if got := load(t, "--watch-namespaces", "").List("watch-namespaces"); got != nil {
		t.Errorf("List(empty) = %q, want nil", got)
	}
}

func TestLogLevel(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		env     map[string]string
		want    slog.Level
		wantErr bool
	}{
		{"default is info", nil, nil, slog.LevelInfo, false},
		{"flag sets level", []string{"--log-level", "error"}, nil, slog.LevelError, false},
		{"env sets level", nil, map[string]string{"DEX_CLIENT_CONTROLLER_LOG_LEVEL": "debug"}, slog.LevelDebug, false},
		{"unknown level errors", []string{"--log-level", "loud"}, nil, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			o, cmd := newBound(t)
			cmd.SetArgs(tt.args)
			if err := cmd.Execute(); err != nil {
				t.Fatalf("execute: %v", err)
			}
			err := o.Load(cmd)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Load: error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && o.LogLevel.Level() != tt.want {
				t.Errorf("LogLevel = %v, want %v", o.LogLevel.Level(), tt.want)
			}
		})
	}
}
