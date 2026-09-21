package central

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

const (
	maxQuotaResetBytes  = 4 << 10
	maxResetNoteLength  = 255
	rejectedResetReason = "a quota reset needs an id of 1 to 64 characters and an optional note of up to 255"
	rejectedPlateReason = "the plate is not in a shape any lane reads"
)

type operator struct {
	store *Store
	month billingMonth
}

type plateLookupResponse struct {
	Plate            string                `json:"plate"`
	OwnerType        *string               `json:"owner_type"`
	OwnerName        *string               `json:"owner_name"`
	LeasingCompany   *string               `json:"leasing_company"`
	Subscription     *subscriptionResponse `json:"subscription"`
	WashesThisMonth  int                   `json:"washes_this_month"`
	WashesSinceReset int                   `json:"washes_since_reset"`
	QuotaResetAt     *time.Time            `json:"quota_reset_at"`
	Washes           []plateWashResponse   `json:"washes"`
}

type subscriptionResponse struct {
	Plan   Plan   `json:"plan"`
	Status string `json:"status"`
}

type plateWashResponse struct {
	AdmittedAt time.Time `json:"admitted_at"`
	SiteID     string    `json:"site_id"`
}

type quotaResetRequest struct {
	ID   string `json:"id"`
	Note string `json:"note"`
}

type quotaResetResponse struct {
	ID      string    `json:"id"`
	Plate   string    `json:"plate"`
	ResetAt time.Time `json:"reset_at"`
	Note    *string   `json:"note"`
}

type sitesResponse struct {
	Sites []siteResponse `json:"sites"`
}

type siteResponse struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	LastSyncedAt *time.Time `json:"last_synced_at"`
}

func newOperator(store *Store) *operator {
	return &operator{store: store, month: newBillingMonth()}
}

func (o *operator) handleGetPlate(w http.ResponseWriter, r *http.Request) {
	plate, isWellFormed := canonicalPlate(r.PathValue("plate"))
	if !isWellFormed {
		http.Error(w, rejectedPlateReason, http.StatusBadRequest)
		return
	}
	lookup, err := o.store.LookupPlate(r.Context(), plate, o.month.start())
	if errors.Is(err, ErrUnknownPlate) {
		http.Error(w, "central holds no vehicle with this plate", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("plate lookup failed", "error", err)
		http.Error(w, "central could not look up this plate", http.StatusInternalServerError)
		return
	}
	slog.Info("plate looked up", "washes_this_month", len(lookup.Washes), "is_subscribed", lookup.Subscription != nil)

	response := plateLookupResponse{
		Plate:            lookup.Plate,
		OwnerType:        optionalString(string(lookup.OwnerType)),
		OwnerName:        optionalString(lookup.OwnerName),
		LeasingCompany:   optionalString(lookup.LeasingCompany),
		WashesThisMonth:  len(lookup.Washes),
		WashesSinceReset: lookup.WashesSinceReset,
		QuotaResetAt:     optionalTime(lookup.QuotaResetAt),
		Washes:           make([]plateWashResponse, 0, len(lookup.Washes)),
	}
	if lookup.Subscription != nil {
		response.Subscription = &subscriptionResponse{Plan: lookup.Subscription.Plan, Status: lookup.Subscription.Status}
	}
	for _, wash := range lookup.Washes {
		response.Washes = append(response.Washes, plateWashResponse{AdmittedAt: wash.AdmittedAt, SiteID: wash.SiteID})
	}
	writeJSON(w, http.StatusOK, response)
}

func (o *operator) handlePostQuotaReset(w http.ResponseWriter, r *http.Request) {
	plate, isWellFormed := canonicalPlate(r.PathValue("plate"))
	if !isWellFormed {
		http.Error(w, rejectedPlateReason, http.StatusBadRequest)
		return
	}
	var body quotaResetRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxQuotaResetBytes))
	if err := decoder.Decode(&body); err != nil || decoder.More() ||
		!isPresentWithin(body.ID, maxReferenceLength) || len(body.Note) > maxResetNoteLength {
		http.Error(w, rejectedResetReason, http.StatusBadRequest)
		return
	}

	reset, isNew, err := o.store.ResetQuota(r.Context(), QuotaReset{ID: body.ID, Plate: plate, ResetAt: o.month.now(), Note: body.Note})
	switch {
	case errors.Is(err, ErrNotSubscribed):
		http.Error(w, "the plate holds no active subscription to reset", http.StatusNotFound)
		return
	case errors.Is(err, ErrNotPremium):
		http.Error(w, "only a Premium plate has a monthly cap to reset", http.StatusConflict)
		return
	case errors.Is(err, ErrResetIDTaken):
		http.Error(w, "this reset id was already used for another plate", http.StatusConflict)
		return
	case err != nil:
		slog.Error("quota reset failed", "reset_id", body.ID, "error", err)
		http.Error(w, "central could not record this quota reset", http.StatusInternalServerError)
		return
	}
	slog.Info("quota reset", "reset_id", reset.ID, "is_new", isNew)

	status := http.StatusOK
	if isNew {
		status = http.StatusCreated
	}
	writeJSON(w, status, quotaResetResponse{ID: reset.ID, Plate: reset.Plate, ResetAt: reset.ResetAt, Note: optionalString(reset.Note)})
}

func (o *operator) handleGetSites(w http.ResponseWriter, r *http.Request) {
	sites, err := o.store.Sites(r.Context())
	if err != nil {
		slog.Error("site listing failed", "error", err)
		http.Error(w, "central could not list sites", http.StatusInternalServerError)
		return
	}
	response := sitesResponse{Sites: make([]siteResponse, 0, len(sites))}
	for _, site := range sites {
		response.Sites = append(response.Sites, siteResponse{ID: site.ID, Name: site.Name, LastSyncedAt: optionalTime(site.LastSyncedAt)})
	}
	writeJSON(w, http.StatusOK, response)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
