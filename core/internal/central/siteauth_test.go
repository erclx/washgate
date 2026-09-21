package central

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

var (
	goodToken  = strings.Repeat("a", minSiteTokenLength)
	otherToken = strings.Repeat("b", minSiteTokenLength)
)

func echoSite(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte(authenticatedSite(r.Context())))
}

func requestAsSite(t *testing.T, handler http.Handler, authorization string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/entitlements", nil)
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestRequireSite(t *testing.T) {
	t.Run("a valid token reaches the handler carrying its site id", func(t *testing.T) {
		store, _ := newTestStore(t)
		token := provisionSite(t, store, testSiteID)

		recorder := requestAsSite(t, requireSite(store, echoSite), "Bearer "+token)

		if recorder.Code != http.StatusOK || recorder.Body.String() != testSiteID {
			t.Fatalf("status %d body %q, want 200 and %q", recorder.Code, recorder.Body.String(), testSiteID)
		}
	})

	cases := []struct {
		name          string
		authorization string
	}{
		{name: "no Authorization header", authorization: ""},
		{name: "a token no site holds", authorization: "Bearer " + otherToken},
		{name: "a scheme other than Bearer", authorization: "Basic " + goodToken},
		{name: "an empty bearer token", authorization: "Bearer "},
	}
	for _, testCase := range cases {
		t.Run("answers 401 to "+testCase.name, func(t *testing.T) {
			store, _ := newTestStore(t)
			provisionSite(t, store, testSiteID)

			recorder := requestAsSite(t, requireSite(store, echoSite), testCase.authorization)

			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
			}
			if got := recorder.Header().Get("WWW-Authenticate"); got != "Bearer" {
				t.Fatalf("WWW-Authenticate = %q, want Bearer", got)
			}
		})
	}
}

func TestParseSiteTokens(t *testing.T) {
	t.Run("parses each site and its token", func(t *testing.T) {
		tokens, err := ParseSiteTokens("site-1=" + goodToken + ", site-2=" + otherToken)

		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		want := []SiteToken{{SiteID: "site-1", Token: goodToken}, {SiteID: "site-2", Token: otherToken}}
		if !slices.Equal(tokens, want) {
			t.Fatalf("tokens = %+v, want %+v", tokens, want)
		}
	})

	t.Run("trims spaces around each site id and token", func(t *testing.T) {
		tokens, err := ParseSiteTokens("site-1 = " + goodToken + " ")

		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if want := []SiteToken{{SiteID: "site-1", Token: goodToken}}; !slices.Equal(tokens, want) {
			t.Fatalf("tokens = %+v, want %+v", tokens, want)
		}
	})

	t.Run("an empty value provisions nothing", func(t *testing.T) {
		tokens, err := ParseSiteTokens("")

		if err != nil || len(tokens) != 0 {
			t.Fatalf("tokens = %+v, error %v, want none and no error", tokens, err)
		}
	})

	cases := []struct {
		name string
		raw  string
	}{
		{name: "an entry with no equals sign", raw: "site-1"},
		{name: "an empty site id", raw: "=" + goodToken},
		{name: "a token under 32 characters", raw: "site-1=" + goodToken[1:]},
		{name: "an empty entry", raw: "site-1=" + goodToken + ","},
		{name: "a site named twice", raw: "site-1=" + goodToken + ",site-1=" + otherToken},
		{name: "one token shared by two sites", raw: "site-1=" + goodToken + ",site-2=" + goodToken},
		{name: "a site id over 64 characters", raw: strings.Repeat("s", maxReferenceLength+1) + "=" + goodToken},
	}
	for _, testCase := range cases {
		t.Run("rejects "+testCase.name, func(t *testing.T) {
			_, err := ParseSiteTokens(testCase.raw)

			if err == nil {
				t.Fatal("error = nil, want a rejection")
			}
		})
	}

	t.Run("a rejection never repeats the token", func(t *testing.T) {
		shortToken := goodToken[1:]

		_, err := ParseSiteTokens("site-1=" + shortToken)

		if err == nil || strings.Contains(err.Error(), shortToken) {
			t.Fatalf("error = %v, want a rejection that omits the token", err)
		}
	})
}
