// Package config resolves the webhookd runtime configuration from CLI
// flags, environment variables, and built-in defaults.
//
// Precedence, highest first, per docs/webhookd-core.md §"Config
// resolution order":
//
//  1. CLI flag, if the user changed it
//  2. Environment variable, if set
//  3. Built-in default
//
// "Changed" is the operative word for the first level: a flag whose
// value equals the default is indistinguishable from an unset flag
// unless the framework tells us, and cobra's Flags().Changed() does.
// That matters for --pretty, where --pretty=false is a real choice and
// must beat a WEBHOOKD_PRETTY=true environment variable.
//
// The secret value itself never enters a Config. Config stores the
// *name* of the environment variable that holds the secret. The
// provider reads the value at verification time, so the secret is not
// present in any process memory the core can leak through a diagnostic
// or a log line.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// Built-in defaults. Named rather than inlined so a reader can find
// the source of any default value by searching for the constant name.
const (
	DefaultPort    = 8080
	DefaultHost    = "127.0.0.1"
	DefaultPretty  = false
	DefaultMaxBody = 2 * 1024 * 1024 // 2 MB, matching the spec's default
	DefaultTimeout = 10 * time.Second

	// EnvPrefix namespaces every environment variable that overrides a
	// flag. WEBHOOKD_PORT overrides --port, WEBHOOKD_HOST overrides
	// --host, and so on. The one flag without an env-var override is
	// --secret-env, which *names* another env var rather than taking a
	// value from one.
	EnvPrefix = "WEBHOOKD_"
)

// maxPort is the largest valid TCP port number.
const maxPort = 65535

// Config is the fully-resolved runtime configuration.
//
// Every field is populated by Load. No caller re-derives defaults.
type Config struct {
	// ProviderName is the subcommand the binary was invoked with,
	// e.g. "mock" or "github". It is the registry key, the "provider"
	// field in output, and the basis for the default Path.
	ProviderName string

	// SecretEnv is the name of the environment variable that holds
	// the signing secret. Empty means the provider must supply its own
	// default (declared in its cmd/<name>.go) or fail.
	SecretEnv string

	// Port is the TCP port the server binds. 0 asks the operating
	// system to choose.
	Port int

	// Host is the bind address.
	Host string

	// Path is the URL path the webhook handler is mounted at. Always
	// begins with "/".
	Path string

	// Pretty selects human-readable output over JSONL.
	Pretty bool

	// MaxBody is the request body size cap in bytes.
	MaxBody int64

	// Timeout is applied as both ReadTimeout and WriteTimeout on the
	// HTTP server.
	Timeout time.Duration
}

// Load resolves a Config for the named provider.
//
// cmd must already have the shared flags declared on it. In practice
// that is done by cmd/root.go before this function is called. cmd must
// not be nil.
//
// providerName must be non-empty. It is used to compute the default
// Path, so a missing name produces a wrong default, not just an ugly
// label.
func Load(cmd *cobra.Command, providerName string) (*Config, error) {
	if cmd == nil {
		return nil, errors.New("config: nil command")
	}
	if providerName == "" {
		return nil, errors.New("config: empty provider name")
	}

	cfg := &Config{ProviderName: providerName}

	// --secret-env: the name of an environment variable. The default
	// is "", which means "no secret; the provider must know its own
	// default." A provider's cmd/<name>.go is the place to supply that
	// default, not this package.
	cfg.SecretEnv = stringValue(cmd, "secret-env", EnvPrefix+"SECRET_ENV", "")

	// --port
	port, err := intValue(cmd, "port", EnvPrefix+"PORT", DefaultPort)
	if err != nil {
		return nil, err
	}
	if port < 0 || port > maxPort {
		return nil, fmt.Errorf("config: port %d out of range [0, %d]", port, maxPort)
	}
	cfg.Port = port

	// --host
	cfg.Host = stringValue(cmd, "host", EnvPrefix+"HOST", DefaultHost)

	// --path
	defaultPath := "/" + providerName
	cfg.Path = stringValue(cmd, "path", EnvPrefix+"PATH", defaultPath)
	if !strings.HasPrefix(cfg.Path, "/") {
		return nil, fmt.Errorf("config: path %q must begin with /", cfg.Path)
	}

	// --pretty
	pretty, err := boolValue(cmd, "pretty", EnvPrefix+"PRETTY", DefaultPretty)
	if err != nil {
		return nil, err
	}
	cfg.Pretty = pretty

	// --max-body
	maxBody, err := int64Value(cmd, "max-body", EnvPrefix+"MAX_BODY", DefaultMaxBody)
	if err != nil {
		return nil, err
	}
	if maxBody <= 0 {
		return nil, fmt.Errorf("config: max-body %d must be positive", maxBody)
	}
	cfg.MaxBody = maxBody

	// --timeout is expressed in seconds on the CLI but stored as a
	// time.Duration, matching http.Server.ReadTimeout and WriteTimeout.
	timeoutSecs, err := intValue(cmd, "timeout", EnvPrefix+"TIMEOUT", int(DefaultTimeout/time.Second))
	if err != nil {
		return nil, err
	}
	if timeoutSecs <= 0 {
		return nil, fmt.Errorf("config: timeout %d must be positive", timeoutSecs)
	}
	cfg.Timeout = time.Duration(timeoutSecs) * time.Second

	return cfg, nil
}

// stringValue resolves a string flag. The precedence is flag (if
// changed) > environment variable (if present) > default.
func stringValue(cmd *cobra.Command, flag, envKey, def string) string {
	if cmd.Flags().Changed(flag) {
		v, _ := cmd.Flags().GetString(flag)
		return v
	}
	if v, ok := os.LookupEnv(envKey); ok {
		return v
	}
	return def
}

// intValue resolves an int flag. The error from a malformed
// environment variable is wrapped with the variable name so the user
// knows which one to fix.
func intValue(cmd *cobra.Command, flag, envKey string, def int) (int, error) {
	if cmd.Flags().Changed(flag) {
		v, err := cmd.Flags().GetInt(flag)
		if err != nil {
			return 0, fmt.Errorf("config: flag --%s: %w", flag, err)
		}
		return v, nil
	}
	if raw, ok := os.LookupEnv(envKey); ok {
		v, err := strconv.Atoi(raw)
		if err != nil {
			return 0, fmt.Errorf("config: env %s=%q is not an integer", envKey, raw)
		}
		return v, nil
	}
	return def, nil
}

// int64Value is the same as intValue but for int64 flags.
func int64Value(cmd *cobra.Command, flag, envKey string, def int64) (int64, error) {
	if cmd.Flags().Changed(flag) {
		v, err := cmd.Flags().GetInt64(flag)
		if err != nil {
			return 0, fmt.Errorf("config: flag --%s: %w", flag, err)
		}
		return v, nil
	}
	if raw, ok := os.LookupEnv(envKey); ok {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("config: env %s=%q is not an integer", envKey, raw)
		}
		return v, nil
	}
	return def, nil
}

// boolValue resolves a bool flag.
//
// Environment values accept the same spellings as strconv.ParseBool:
// 1, t, T, TRUE, true, True, 0, f, F, FALSE, false, False. Anything
// else is an error, so a typo like WEBHOOKD_PRETTY=yes fails loudly
// rather than silently meaning false.
func boolValue(cmd *cobra.Command, flag, envKey string, def bool) (bool, error) {
	if cmd.Flags().Changed(flag) {
		v, err := cmd.Flags().GetBool(flag)
		if err != nil {
			return false, fmt.Errorf("config: flag --%s: %w", flag, err)
		}
		return v, nil
	}
	if raw, ok := os.LookupEnv(envKey); ok {
		v, err := strconv.ParseBool(raw)
		if err != nil {
			return false, fmt.Errorf("config: env %s=%q is not a boolean", envKey, raw)
		}
		return v, nil
	}
	return def, nil
}
