package siteagent

import (
	"log/slog"
	"net/http"
	"sync"
	"time"
)

const maxLinkBodyBytes = 1 << 10

// LinkState is the site's link to central as the dashboard shows it.
type LinkState string

// Link states.
const (
	LinkOnline  LinkState = "online"
	LinkOffline LinkState = "offline"
	// LinkCut is a reviewer cutting the link from the dashboard, which the syncer obeys.
	LinkCut LinkState = "cut"
)

// LinkSwitch holds the site's link to central as the syncer last found it, and the switch a reviewer
// cuts it with. It lives in memory, so a restart restores the link. The zero value is an uncut link
// that has not synced yet.
type LinkSwitch struct {
	mu         sync.Mutex
	isCut      bool
	isSyncing  bool
	hasSynced  bool
	lastFailed bool
}

// SetCut cuts the link or restores it.
func (l *LinkSwitch) SetCut(isCut bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.isCut = isCut
}

// IsCut reports whether a reviewer has cut the link.
func (l *LinkSwitch) IsCut() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.isCut
}

func (l *LinkSwitch) beginSync() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.isSyncing = true
}

func (l *LinkSwitch) endSync(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.isSyncing = false
	l.hasSynced = true
	l.lastFailed = err != nil
}

// state reports the link and whether a sync is running. A link that has not synced yet reads offline.
func (l *LinkSwitch) state() (LinkState, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	switch {
	case l.isCut:
		return LinkCut, l.isSyncing
	case !l.hasSynced || l.lastFailed:
		return LinkOffline, l.isSyncing
	default:
		return LinkOnline, l.isSyncing
	}
}

// siteStatus answers for the site's own health, a role neither the lane nor the syncer holds.
type siteStatus struct {
	store  *Store
	config Config
}

type statusResponse struct {
	SiteID       string     `json:"site_id"`
	Link         LinkState  `json:"link"`
	OutboxDepth  int        `json:"outbox_depth"`
	LastSyncedAt *time.Time `json:"last_synced_at"`
	IsSyncing    bool       `json:"is_syncing"`
}

// handleStatus reports what only the site knows about its link to central: its state, the washes
// central has not acknowledged, and when the copy last pulled.
func (s *siteStatus) handleStatus(w http.ResponseWriter, r *http.Request) {
	depth, err := s.store.OutboxDepth(r.Context())
	if err != nil {
		slog.Error("site status failed", "site_id", s.config.SiteID, "error", err)
		http.Error(w, "the site could not read its status", http.StatusInternalServerError)
		return
	}
	lastPulledAt, err := s.store.LastPulledAt(r.Context())
	if err != nil {
		slog.Error("site status failed", "site_id", s.config.SiteID, "error", err)
		http.Error(w, "the site could not read its status", http.StatusInternalServerError)
		return
	}
	link, isSyncing := s.config.Link.state()
	response := statusResponse{SiteID: s.config.SiteID, Link: link, OutboxDepth: depth, IsSyncing: isSyncing}
	if !lastPulledAt.IsZero() {
		response.LastSyncedAt = &lastPulledAt
	}
	writeJSON(w, response)
}

type linkRequest struct {
	Cut *bool `json:"cut"`
}

// handleLink cuts the site's link to central or restores it.
func (s *siteStatus) handleLink(w http.ResponseWriter, r *http.Request) {
	var body linkRequest
	if !decodeJSONBody(w, r, maxLinkBodyBytes, &body) {
		return
	}
	if body.Cut == nil {
		http.Error(w, `the link switch needs "cut" as true or false`, http.StatusBadRequest)
		return
	}
	s.config.Link.SetCut(*body.Cut)
	slog.Info("central link switched", "site_id", s.config.SiteID, "cut", *body.Cut)
	w.WriteHeader(http.StatusNoContent)
}
