// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package cli

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// envPrefix namespaces every environment variable: flag --dex-address is
// DEX_CLIENT_CONTROLLER_DEX_ADDRESS, and so on (dashes become underscores).
const envPrefix = "DEX_CLIENT_CONTROLLER"

// NewOptions builds Options with the conventional stderr logger and shared
// verbosity level.
func NewOptions() *Options {
	level := new(slog.LevelVar)
	return &Options{
		Log:      slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})),
		LogLevel: level,
	}
}

// NewControllerRoot builds the root command with the shared options bound and
// flag/env resolution wired into PersistentPreRunE.
func NewControllerRoot(name, short string, opts *Options) *cobra.Command {
	root := NewRoot(name, short)
	opts.Bind(root)
	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error { return opts.Load(cmd) }
	return root
}

// Options carries the process-wide configuration.
// Precedence: explicit flag > DEX_CLIENT_CONTROLLER_* environment > default.
type Options struct {
	// Log is the process logger.
	Log *slog.Logger
	// LogLevel is set from --log-level before any command runs.
	LogLevel *slog.LevelVar
	// LogLevelName is the configured level: debug, info, warn, or error.
	LogLevelName string

	viper *viper.Viper
}

// Bind registers the shared persistent flags on cmd and wires the viper
// environment binding. Subcommand flags are read through the typed accessors
// below with the same precedence.
func (o *Options) Bind(cmd *cobra.Command) {
	pf := cmd.PersistentFlags()
	pf.StringVar(&o.LogLevelName, "log-level", "info", "log level: debug, info, warn, or error")

	o.viper = viper.New()
	o.viper.SetEnvPrefix(envPrefix)
	o.viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	o.viper.AutomaticEnv()
}

// Load resolves flag/env precedence into the Options fields. Run it in
// PersistentPreRunE, after cobra parsed the flags.
func (o *Options) Load(cmd *cobra.Command) error {
	if err := o.viper.BindPFlags(cmd.Flags()); err != nil {
		return fmt.Errorf("bind flags: %w", err)
	}
	o.LogLevelName = o.viper.GetString("log-level")
	var level slog.Level
	if err := level.UnmarshalText([]byte(o.LogLevelName)); err != nil {
		return fmt.Errorf("log level: %q is not debug, info, warn, or error", o.LogLevelName)
	}
	if o.LogLevel != nil {
		o.LogLevel.Set(level)
	}
	return nil
}

// String reads a subcommand value, applying flag/env precedence.
func (o *Options) String(key string) string { return o.viper.GetString(key) }

// Bool reads a subcommand boolean value.
func (o *Options) Bool(key string) bool { return o.viper.GetBool(key) }

// Duration reads a subcommand duration value.
func (o *Options) Duration(key string) time.Duration { return o.viper.GetDuration(key) }

// List reads a comma-separated subcommand value as a list, dropping blanks.
// viper's own slice reader splits environment values on whitespace, not
// commas, so a flag and its environment twin would disagree.
func (o *Options) List(key string) []string {
	var out []string
	for _, v := range strings.Split(o.viper.GetString(key), ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}
