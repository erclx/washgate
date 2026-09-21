package washctl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// maxResponseBytes bounds what the CLI holds in memory from one call. A month of fleet invoicing is far below it.
const maxResponseBytes = 64 << 20

// refusalError is a service saying no, carrying what it said so the operator knows what to change.
type refusalError struct {
	status  int
	message string
}

func (e *refusalError) Error() string {
	switch e.status {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return "refused: " + e.message
	case http.StatusNotFound:
		return "not found: " + e.message
	case http.StatusConflict:
		return "conflict: " + e.message
	default:
		return fmt.Sprintf("the service answered %d: %s", e.status, e.message)
	}
}

type response struct {
	status int
	body   []byte
}

// client is the one place that builds requests and applies a timeout.
type client struct {
	http *http.Client
}

func newClient(timeout time.Duration) client {
	return client{http: &http.Client{Timeout: timeout}}
}

// get returns the body of a 2xx answer and a refusalError for any other status.
func (c client) get(ctx context.Context, url string) (response, error) {
	return c.do(ctx, http.MethodGet, url, nil)
}

func (c client) postJSON(ctx context.Context, url string, body any) (response, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return response{}, fmt.Errorf("encode request: %w", err)
	}
	return c.do(ctx, http.MethodPost, url, encoded)
}

func (c client) do(ctx context.Context, method, url string, body []byte) (response, error) {
	request, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return response{}, fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	answer, err := c.http.Do(request)
	if err != nil {
		return response{}, fmt.Errorf("reach %s: %w", url, err)
	}
	defer func() { _ = answer.Body.Close() }()

	data, err := io.ReadAll(io.LimitReader(answer.Body, maxResponseBytes+1))
	if err != nil {
		return response{}, fmt.Errorf("read answer from %s: %w", url, err)
	}
	if len(data) > maxResponseBytes {
		return response{}, fmt.Errorf("the answer from %s is over %d bytes", url, maxResponseBytes)
	}
	if answer.StatusCode < 200 || answer.StatusCode > 299 {
		return response{}, &refusalError{status: answer.StatusCode, message: strings.TrimSpace(string(data))}
	}
	return response{status: answer.StatusCode, body: data}, nil
}

// isRetryable says whether a failure could vanish on a second try: the call never got an answer,
// or the service answered with a server error. A refusal never changes on repeat.
func isRetryable(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	var refusal *refusalError
	if errors.As(err, &refusal) {
		return refusal.status >= http.StatusInternalServerError
	}
	return true
}
