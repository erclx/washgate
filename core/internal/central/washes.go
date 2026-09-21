package central

import (
	"encoding/json"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"time"
)

const (
	maxWashesPerBatch  = 500
	maxWashBodyBytes   = 256 << 10
	maxWashIDLength    = 64
	maxPlateLength     = 16
	maxReferenceLength = 64
	// maxPrepaidWashIDLength fits the Checkout session id a prepaid wash is keyed on.
	maxPrepaidWashIDLength = 255
	washBatchMediaType     = "application/json"
	rejectedBatchReason    = "the batch needs a site_id and 1 to 500 washes, each with an id, a plate, a plan of premium, fleet, or prepaid, and an admitted_at, where only a fleet wash names its company_id and only a prepaid wash names its prepaid_wash_id"
)

type washBatchRequest struct {
	SiteID string        `json:"site_id"`
	Washes []washRequest `json:"washes"`
}

type washRequest struct {
	ID            string    `json:"id"`
	Plate         string    `json:"plate"`
	Plan          Plan      `json:"plan"`
	CompanyID     string    `json:"company_id"`
	PrepaidWashID string    `json:"prepaid_wash_id"`
	AdmittedAt    time.Time `json:"admitted_at"`
}

type washBatchResponse struct {
	Stored     []string `json:"stored"`
	Duplicates []string `json:"duplicates"`
}

type ledger struct {
	store *Store
}

func (l *ledger) handlePostWashes(w http.ResponseWriter, r *http.Request) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != washBatchMediaType {
		http.Error(w, "body must be application/json", http.StatusUnsupportedMediaType)
		return
	}
	var batch washBatchRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxWashBodyBytes))
	if err := decoder.Decode(&batch); err != nil || decoder.More() || !isValidBatch(batch) {
		http.Error(w, rejectedBatchReason, http.StatusBadRequest)
		return
	}
	if siteID := authenticatedSite(r.Context()); batch.SiteID != siteID {
		slog.Warn("wash batch refused", "site_id", siteID, "claimed_site_id", batch.SiteID, "reason", "site_id is another site's")
		http.Error(w, "a site can push washes only under its own site_id", http.StatusForbidden)
		return
	}

	washes := make([]Wash, 0, len(batch.Washes))
	for _, wash := range batch.Washes {
		washes = append(washes, Wash(wash))
	}
	result, err := l.store.RecordWashes(r.Context(), batch.SiteID, washes)
	if errors.Is(err, ErrUnknownSite) || errors.Is(err, ErrUnknownCompany) || errors.Is(err, ErrUnknownPrepaidWash) {
		slog.Warn("wash batch rejected", "site_id", batch.SiteID, "batch_size", len(washes), "reason", err.Error())
		http.Error(w, "the batch names a site, company, or prepaid wash central does not know", http.StatusUnprocessableEntity)
		return
	}
	if err != nil {
		slog.Error("wash batch failed", "site_id", batch.SiteID, "batch_size", len(washes), "error", err)
		http.Error(w, "central could not store this batch", http.StatusInternalServerError)
		return
	}
	slog.Info("wash batch stored", "site_id", batch.SiteID, "batch_size", len(washes),
		"stored", len(result.Stored), "duplicates", len(result.Duplicates))
	for _, prepaidWashID := range result.SpentPrepaidWashes {
		slog.Info("prepaid wash spent", "site_id", batch.SiteID, "prepaid_wash_id", prepaidWashID)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(washBatchResponse{Stored: result.Stored, Duplicates: result.Duplicates})
}

func isValidBatch(batch washBatchRequest) bool {
	if !isPresentWithin(batch.SiteID, maxReferenceLength) || len(batch.Washes) == 0 || len(batch.Washes) > maxWashesPerBatch {
		return false
	}
	for _, wash := range batch.Washes {
		if !isValidWash(wash) {
			return false
		}
	}
	return true
}

func isValidWash(wash washRequest) bool {
	if !isPresentWithin(wash.ID, maxWashIDLength) || !isPresentWithin(wash.Plate, maxPlateLength) || wash.AdmittedAt.IsZero() {
		return false
	}
	switch wash.Plan {
	case PlanFleet:
		return isPresentWithin(wash.CompanyID, maxReferenceLength) && wash.PrepaidWashID == ""
	case PlanPremium:
		return wash.CompanyID == "" && wash.PrepaidWashID == ""
	case PlanPrepaid:
		return wash.CompanyID == "" && isPresentWithin(wash.PrepaidWashID, maxPrepaidWashIDLength)
	default:
		return false
	}
}

func isPresentWithin(value string, maxLength int) bool {
	return value != "" && len(value) <= maxLength
}
