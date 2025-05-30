package tenantauth

import (
	"net/http"
	"strings"

	"go.opentelemetry.io/collector/client"
)

// BearerAuthRoundTripper intercepts and adds Bearer token Authorization headers to each http request.
type BearerAuthRoundTripper struct {
	header string
	base   http.RoundTripper
	// auth          *BearerTokenAuth
}

const (
	// prometheusRemoteWrite
	// https://<vminsert-addr>/insert/{TENANT_ID}/prometheus/api/v1/write
	TenantIDPlaceholder = "{TENANT_ID}"
)

// RoundTrip modifies the original request and adds Bearer token Authorization headers. Incoming requests support multiple tokens, but outgoing requests only use one.
func (interceptor *BearerAuthRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	if req2.Header == nil {
		req2.Header = make(http.Header)
	}

	info := client.FromContext(req.Context())
	v := info.Metadata.Get("account_id")
	if len(v) == 0 {
		return interceptor.base.RoundTrip(req2)
	}

	accountID := v[0]
	if len(accountID) > 0 {
		req2.URL.Path = strings.ReplaceAll(req.URL.Path, TenantIDPlaceholder, accountID)
	}

	// req2.Header.Set(interceptor.header, interceptor.auth.authorizationValue())
	return interceptor.base.RoundTrip(req2)
}
