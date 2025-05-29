// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package tenantauth // import "github.com/CloudDetail/apo-otel-collector/pkg/extension/tenantextension"

import (
	"errors"

	"go.opentelemetry.io/collector/component"
)

// Config specifies how the Per-RPC bearer token based authentication data should be obtained.
type Config struct {
	// Header specifies the auth-header for the token. Defaults to "Authorization"
	Header string `mapstructure:"header,omitempty"`

	// Scheme specifies the auth-scheme for the token. Defaults to "Bearer"
	Scheme string `mapstructure:"scheme,omitempty"`

	// // BearerToken specifies the bearer token to use for every RPC.
	// BearerToken configopaque.String `mapstructure:"token,omitempty"`

	// // Tokens specifies multiple bearer tokens to use for every RPC.
	// Tokens []configopaque.String `mapstructure:"tokens,omitempty"`

	// // Filename points to a file that contains the bearer token(s) to use for every RPC.
	// Filename string `mapstructure:"filename,omitempty"`

	// Obtain the public key from a JWKs URI
	JWKsURI string `mapstructure:"jwks_uri,omitempty"`

	PublicKey string `mapstructure:"public_key,omitempty"`
	// TODO Obtain the public key from an environment variable
	PublicKeyEnv string `mapstructure:"public_key_env,omitempty"`
	// TODO Obtain the public key from a file
	PublicKeyFile string `mapstructure:"public_key_file,omitempty"`

	// prevent unkeyed literal initialization
	_ struct{}
}

var (
	_                      component.Config = (*Config)(nil)
	errNoPublicKeyProvided                  = errors.New("no publicKey provided")
)

// Validate checks if the extension configuration is valid
func (cfg *Config) Validate() error {
	if len(cfg.JWKsURI) == 0 && len(cfg.PublicKey) == 0 && len(cfg.PublicKeyEnv) == 0 && len(cfg.PublicKeyFile) == 0 {
		return errNoPublicKeyProvided
	}
	return nil
}
