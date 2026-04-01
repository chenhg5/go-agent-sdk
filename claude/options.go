package claude

import (
	"net/http"
	"time"
)

const (
	defaultBaseURL    = "https://api.anthropic.com"
	defaultAPIVersion = "2023-06-01"
	defaultTimeout    = 5 * time.Minute
	defaultMaxRetries = 2
)

type options struct {
	baseURL      string
	apiVersion   string
	httpClient   *http.Client
	maxRetries   int
	betaFeatures []string
	extraHeaders map[string]string
}

func defaultOptions() options {
	return options{
		baseURL:    defaultBaseURL,
		apiVersion: defaultAPIVersion,
		httpClient: &http.Client{Timeout: defaultTimeout},
		maxRetries: defaultMaxRetries,
	}
}

// Option configures the Claude provider.
type Option func(*options)

// WithBaseURL overrides the API base URL (e.g. for proxies or compatible APIs).
func WithBaseURL(url string) Option {
	return func(o *options) { o.baseURL = url }
}

// WithAPIVersion overrides the anthropic-version header.
func WithAPIVersion(v string) Option {
	return func(o *options) { o.apiVersion = v }
}

// WithHTTPClient replaces the default HTTP client.
func WithHTTPClient(c *http.Client) Option {
	return func(o *options) { o.httpClient = c }
}

// WithMaxRetries sets the maximum number of retries on retryable errors.
func WithMaxRetries(n int) Option {
	return func(o *options) { o.maxRetries = n }
}

// WithBetaFeatures adds beta feature header values.
func WithBetaFeatures(features ...string) Option {
	return func(o *options) { o.betaFeatures = append(o.betaFeatures, features...) }
}

// WithExtraHeaders adds custom HTTP headers to every request.
func WithExtraHeaders(headers map[string]string) Option {
	return func(o *options) { o.extraHeaders = headers }
}
