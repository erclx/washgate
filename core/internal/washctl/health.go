package washctl

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"sync"
	"text/tabwriter"
	"time"
)

const healthUsage = "usage: washctl health [--json]"

// Health states a site row can carry. An unreachable site is the offline case central expects, not a failure.
const (
	stateOK            = "ok"
	stateUnreachable   = "unreachable"
	stateNotConfigured = "not_configured"
)

type sitesAnswer struct {
	Sites []centralSite `json:"sites"`
}

type centralSite struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	LastSyncedAt *time.Time `json:"last_synced_at"`
}

type siteStatus struct {
	OutboxDepth  int        `json:"outbox_depth"`
	LastSyncedAt *time.Time `json:"last_synced_at"`
}

// healthRow joins central's view of a site, when it last heard from it, with the site's own,
// its backlog and when it last pulled.
type healthRow struct {
	SiteID       string     `json:"site_id"`
	Name         string     `json:"name"`
	State        string     `json:"state"`
	LastSyncedAt *time.Time `json:"last_synced_at"`
	OutboxDepth  *int       `json:"outbox_depth"`
	LastPulledAt *time.Time `json:"last_pulled_at"`
}

func runHealth(ctx context.Context, args []string, env *environment) int {
	flags := flag.NewFlagSet("health", flag.ContinueOnError)
	isJSON := flags.Bool("json", false, "print one JSON row per site")
	positionals, err := parseArgs(flags, args)
	if err != nil || len(positionals) != 0 {
		return env.failUsage("%s", healthUsage)
	}

	answer, err := env.central.get(ctx, env.config.CentralURL+"/sites")
	if err != nil {
		return env.fail(err)
	}
	var sites sitesAnswer
	if err := json.Unmarshal(answer.body, &sites); err != nil {
		return env.fail(fmt.Errorf("read central's site list: %w", err))
	}

	rows := env.askSites(ctx, sites.Sites)
	if *isJSON {
		encoder := json.NewEncoder(env.stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(rows); err != nil {
			return env.fail(err)
		}
		return exitOK
	}
	printHealth(env, rows)
	return exitOK
}

// askSites asks every configured site for its status at once, so one offline site costs one timeout rather than one each.
func (e *environment) askSites(ctx context.Context, sites []centralSite) []healthRow {
	urls := make(map[string]string, len(e.config.Sites))
	for _, site := range e.config.Sites {
		urls[site.ID] = site.URL
	}
	rows := make([]healthRow, len(sites))
	var group sync.WaitGroup
	for index, site := range sites {
		group.Go(func() {
			rows[index] = e.askSite(ctx, site, urls[site.ID])
		})
	}
	group.Wait()
	return rows
}

func (e *environment) askSite(ctx context.Context, site centralSite, siteURL string) healthRow {
	row := healthRow{SiteID: site.ID, Name: site.Name, LastSyncedAt: site.LastSyncedAt}
	if siteURL == "" {
		row.State = stateNotConfigured
		return row
	}
	answer, err := e.site.get(ctx, siteURL+"/status")
	if err != nil {
		row.State = stateUnreachable
		return row
	}
	var status siteStatus
	if err := json.Unmarshal(answer.body, &status); err != nil {
		row.State = stateUnreachable
		return row
	}
	row.State = stateOK
	row.OutboxDepth = &status.OutboxDepth
	row.LastPulledAt = status.LastSyncedAt
	return row
}

func printHealth(env *environment, rows []healthRow) {
	table := tabwriter.NewWriter(env.stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(table, "SITE\tNAME\tLAST SYNC\tOUTBOX\tLAST PULL")
	for _, row := range rows {
		outbox, lastPull := row.State, row.State
		if row.State == stateOK {
			outbox, lastPull = fmt.Sprint(*row.OutboxDepth), formatTime(row.LastPulledAt)
		}
		if row.State == stateNotConfigured {
			outbox, lastPull = "not configured", "not configured"
		}
		_, _ = fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\n", row.SiteID, row.Name, formatTime(row.LastSyncedAt), outbox, lastPull)
	}
	_ = table.Flush()
}
