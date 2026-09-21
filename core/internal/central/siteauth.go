package central

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

// minSiteTokenLength keeps a site token long enough that a single SHA-256 is as good as a slow hash.
const minSiteTokenLength = 32

// SiteToken pairs a site with the bearer token it presents to central.
type SiteToken struct {
	SiteID string
	Token  string
}

type siteContextKey struct{}

// ParseSiteTokens reads a comma-separated list of site-id=token pairs. An empty value names no site.
// A rejection names the entry and the site, never the token.
func ParseSiteTokens(raw string) ([]SiteToken, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	entries := strings.Split(raw, ",")
	tokens := make([]SiteToken, 0, len(entries))
	seenSites := map[string]bool{}
	seenTokens := map[string]bool{}
	for index, entry := range entries {
		position := index + 1
		siteID, token, hasSeparator := strings.Cut(entry, "=")
		siteID, token = strings.TrimSpace(siteID), strings.TrimSpace(token)
		switch {
		case !hasSeparator:
			return nil, fmt.Errorf("site token entry %d is not site-id=token", position)
		case !isPresentWithin(siteID, maxReferenceLength):
			return nil, fmt.Errorf("site token entry %d needs a site id of 1 to %d characters", position, maxReferenceLength)
		case len(token) < minSiteTokenLength:
			return nil, fmt.Errorf("site token entry %d gives %s a token under %d characters", position, siteID, minSiteTokenLength)
		case seenSites[siteID]:
			return nil, fmt.Errorf("site token entry %d names %s a second time", position, siteID)
		case seenTokens[token]:
			return nil, fmt.Errorf("site token entry %d gives %s a token another site already holds", position, siteID)
		}
		seenSites[siteID] = true
		seenTokens[token] = true
		tokens = append(tokens, SiteToken{SiteID: siteID, Token: token})
	}
	return tokens, nil
}

func hashSiteToken(token string) [sha256.Size]byte {
	return sha256.Sum256([]byte(token))
}

// requireSite lets a request through to next only when it carries a bearer token a site holds,
// and puts that site's id in the request context.
func requireSite(store *Store, next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, hasBearer := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !hasBearer || token == "" {
			slog.Warn("site request refused", "path", r.URL.Path, "reason", "no bearer token")
			refuseUnauthenticated(w)
			return
		}
		siteID, err := store.SiteForToken(r.Context(), hashSiteToken(token))
		if errors.Is(err, ErrUnknownToken) {
			slog.Warn("site request refused", "path", r.URL.Path, "reason", "token matches no site")
			refuseUnauthenticated(w)
			return
		}
		if err != nil {
			slog.Error("site authentication failed", "path", r.URL.Path, "error", err)
			http.Error(w, "central could not check this site's token", http.StatusInternalServerError)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), siteContextKey{}, siteID)))
	})
}

func refuseUnauthenticated(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	http.Error(w, "this endpoint needs a site's bearer token", http.StatusUnauthorized)
}

// authenticatedSite is the site requireSite let through, or empty outside it.
func authenticatedSite(ctx context.Context) string {
	siteID, _ := ctx.Value(siteContextKey{}).(string)
	return siteID
}
