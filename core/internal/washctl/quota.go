package washctl

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

const quotaResetUsage = "usage: washctl quota-reset [--note text] <plate>"

type quotaResetRequest struct {
	ID   string `json:"id"`
	Note string `json:"note,omitempty"`
}

type quotaResetResult struct {
	ID      string    `json:"id"`
	Plate   string    `json:"plate"`
	ResetAt time.Time `json:"reset_at"`
	Note    *string   `json:"note"`
}

func runQuotaReset(ctx context.Context, args []string, env *environment) int {
	flags := flag.NewFlagSet("quota-reset", flag.ContinueOnError)
	note := flags.String("note", "", "why the quota is being reset, kept as the audit trail")
	plate, isValid := env.parseExactly(flags, args, quotaResetUsage)
	if !isValid {
		return exitUsage
	}

	// One id covers every attempt of this run, so a retry after a lost answer changes nothing at central.
	request := quotaResetRequest{ID: rand.Text(), Note: *note}
	answer, err := env.postWithRetry(ctx, env.config.CentralURL+"/plates/"+url.PathEscape(plate)+"/quota-resets", request)
	if err != nil {
		return env.fail(err)
	}
	var result quotaResetResult
	if err := json.Unmarshal(answer.body, &result); err != nil {
		return env.fail(fmt.Errorf("read central's quota reset answer: %w", err))
	}

	resetAt := result.ResetAt
	if answer.status == http.StatusOK {
		_, _ = fmt.Fprintf(env.stdout, "Quota reset %s was already recorded for %s at %s. Nothing changed.\n",
			result.ID, result.Plate, formatTime(&resetAt))
		return exitOK
	}
	_, _ = fmt.Fprintf(env.stdout, "Recorded quota reset %s for %s at %s.\n", result.ID, result.Plate, formatTime(&resetAt))
	if result.Note != nil {
		_, _ = fmt.Fprintf(env.stdout, "Note: %s\n", *result.Note)
	}
	_, _ = fmt.Fprintln(env.stdout, "Each site counts the cap from the reset once its next pull lands. A site that is offline keeps the old count until it reconnects.")
	return exitOK
}

// postWithRetry sends body up to the configured attempts, backing off between them. It is safe only for
// a write carrying an id the receiver stores, which is what the quota reset sends.
func (e *environment) postWithRetry(ctx context.Context, target string, body any) (response, error) {
	attempts := max(1, e.config.RetryAttempts)
	for attempt := 1; ; attempt++ {
		answer, err := e.central.postJSON(ctx, target, body)
		if err == nil || attempt == attempts || !isRetryable(ctx, err) {
			return answer, err
		}
		select {
		case <-ctx.Done():
			return response{}, ctx.Err()
		case <-time.After(e.config.RetryBackoff * time.Duration(attempt)):
		}
	}
}
