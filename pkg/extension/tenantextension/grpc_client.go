package tenantauth

import (
	"context"

	"google.golang.org/grpc/credentials"
)

// Support otelGrpc exporter !!!
// var _ credentials.PerRPCCredentials = (*PerRPCAuth)(nil)

// PerRPCCredentials returns PerRPCAuth an implementation of credentials.PerRPCCredentials that
func (b *BearerTokenAuth) PerRPCCredentials() (credentials.PerRPCCredentials, error) {
	return &PerRPCAuth{}, nil
}

// PerRPCAuth is a gRPC credentials.PerRPCCredentials implementation that returns an 'authorization' header.
type PerRPCAuth struct{}

// GetRequestMetadata returns the request metadata to be used with the RPC.
func (c *PerRPCAuth) GetRequestMetadata(ctx context.Context, param ...string) (map[string]string, error) {
	// TODO
	return map[string]string{}, nil
}

// RequireTransportSecurity always returns true for this implementation. Passing bearer tokens in plain-text connections is a bad idea.
func (c *PerRPCAuth) RequireTransportSecurity() bool {
	return false
}
