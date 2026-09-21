package central

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

const (
	defaultChangesLimit = 500
	maxChangesLimit     = 1000
)

type entitlementChangesResponse struct {
	Changes []entitlementChangeResponse `json:"changes"`
	Next    int64                       `json:"next"`
}

type entitlementChangeResponse struct {
	Seq          int64      `json:"seq"`
	Plate        string     `json:"plate"`
	Plan         *string    `json:"plan"`
	CompanyID    *string    `json:"company_id"`
	CompanyName  *string    `json:"company_name"`
	QuotaResetAt *time.Time `json:"quota_reset_at"`
}

func (l *ledger) handleGetEntitlements(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	after, isAfterValid := parseQueryNumber(query.Get("after"), 0, 0)
	limit, isLimitValid := parseQueryNumber(query.Get("limit"), defaultChangesLimit, 1)
	if !isAfterValid || !isLimitValid {
		http.Error(w, "after must be a whole number of 0 or more and limit one of 1 or more", http.StatusBadRequest)
		return
	}
	limit = min(limit, maxChangesLimit)

	changes, err := l.store.EntitlementChanges(r.Context(), after, int(limit))
	if err != nil {
		slog.Error("entitlement pull failed", "after", after, "error", err)
		http.Error(w, "central could not read entitlement changes", http.StatusInternalServerError)
		return
	}

	response := entitlementChangesResponse{Changes: make([]entitlementChangeResponse, 0, len(changes)), Next: after}
	for _, change := range changes {
		response.Changes = append(response.Changes, entitlementChangeResponse{
			Seq:          change.Seq,
			Plate:        change.Plate,
			Plan:         optionalString(string(change.Plan)),
			CompanyID:    optionalString(change.CompanyID),
			CompanyName:  optionalString(change.CompanyName),
			QuotaResetAt: optionalTime(change.QuotaResetAt),
		})
		response.Next = change.Seq
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

// parseQueryNumber reads an optional whole number no smaller than minimum, falling back when it is absent.
func parseQueryNumber(raw string, fallback, minimum int64) (int64, bool) {
	if raw == "" {
		return fallback, true
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < minimum {
		return 0, false
	}
	return value, true
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func optionalTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}
