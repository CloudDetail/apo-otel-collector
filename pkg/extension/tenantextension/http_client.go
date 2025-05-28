package tenantauth

import "net/http"

// BearerAuthRoundTripper intercepts and adds Bearer token Authorization headers to each http request.
type BearerAuthRoundTripper struct {
	header string
	base   http.RoundTripper
	// auth          *BearerTokenAuth
}

const (
	// prometheusRemoteWrite
	// https://<vminsert-addr>/insert/<tenant_id>/prometheus/api/v1/write
	RemoteWriteEndpoint = "/insert/{TENANT_ID}/prometheus"
)

// RoundTrip modifies the original request and adds Bearer token Authorization headers. Incoming requests support multiple tokens, but outgoing requests only use one.
func (interceptor *BearerAuthRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	if req2.Header == nil {
		req2.Header = make(http.Header)
	}

	// Do nothing
	// // otlphttp
	// req.URL.Path = ""
	// // victoria metrics
	// req.URL.Path = strings.ReplaceAll(req.URL.Path, RemoteWriteEndpoint, fmt.Sprintf("/insert/{TENANT_ID}/prometheus"))

	// req2.Header.Set(interceptor.header, interceptor.auth.authorizationValue())
	return interceptor.base.RoundTrip(req2)
}
