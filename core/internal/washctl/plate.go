package washctl

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"text/tabwriter"
	"time"
)

const plateUsage = "usage: washctl plate [--json] <plate>"

type plateLookup struct {
	Plate            string     `json:"plate"`
	OwnerType        *string    `json:"owner_type"`
	OwnerName        *string    `json:"owner_name"`
	LeasingCompany   *string    `json:"leasing_company"`
	Subscription     *platePlan `json:"subscription"`
	WashesThisMonth  int        `json:"washes_this_month"`
	WashesSinceReset int        `json:"washes_since_reset"`
	QuotaResetAt     *time.Time `json:"quota_reset_at"`
}

type platePlan struct {
	Plan   string `json:"plan"`
	Status string `json:"status"`
}

func runPlate(ctx context.Context, args []string, env *environment) int {
	flags := flag.NewFlagSet("plate", flag.ContinueOnError)
	isJSON := flags.Bool("json", false, "print central's answer as JSON")
	plate, isValid := env.parseExactly(flags, args, plateUsage)
	if !isValid {
		return exitUsage
	}

	answer, err := env.central.get(ctx, env.config.CentralURL+"/plates/"+url.PathEscape(plate))
	if err != nil {
		return env.fail(err)
	}
	if *isJSON {
		_, _ = env.stdout.Write(answer.body)
		return exitOK
	}
	var lookup plateLookup
	if err := json.Unmarshal(answer.body, &lookup); err != nil {
		return env.fail(fmt.Errorf("read central's plate answer: %w", err))
	}
	printPlate(env, lookup)
	return exitOK
}

func printPlate(env *environment, lookup plateLookup) {
	table := tabwriter.NewWriter(env.stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(table, "Plate:\t%s\n", lookup.Plate)
	_, _ = fmt.Fprintf(table, "Owner:\t%s\n", describeOwner(lookup))
	_, _ = fmt.Fprintf(table, "Plan:\t%s\n", describePlan(lookup.Subscription))
	_, _ = fmt.Fprintf(table, "Leasing company:\t%s\n", valueOrDash(lookup.LeasingCompany))
	_, _ = fmt.Fprintf(table, "Washes this month:\t%d\n", lookup.WashesThisMonth)
	_, _ = fmt.Fprintf(table, "Washes since reset:\t%d\n", lookup.WashesSinceReset)
	_, _ = fmt.Fprintf(table, "Last quota reset:\t%s\n", formatTime(lookup.QuotaResetAt))
	_ = table.Flush()
}

func describeOwner(lookup plateLookup) string {
	if lookup.OwnerName == nil {
		return "no owner on file"
	}
	if lookup.OwnerType == nil {
		return *lookup.OwnerName
	}
	return fmt.Sprintf("%s (%s)", *lookup.OwnerName, *lookup.OwnerType)
}

func describePlan(plan *platePlan) string {
	if plan == nil {
		return "none"
	}
	return fmt.Sprintf("%s (%s)", plan.Plan, plan.Status)
}

func valueOrDash(value *string) string {
	if value == nil {
		return "-"
	}
	return *value
}
