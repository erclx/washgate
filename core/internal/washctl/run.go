// Package washctl is the operations CLI's logic: it looks up plates, resets quotas, fetches monthly invoices,
// and reports site health by calling central, the invoicing service, and each site agent over HTTP.
// It decodes their wire JSON into its own structs and imports no other component.
package washctl

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"
)

// Exit codes: a command that ran and failed is 1, one the operator typed wrongly is 2.
const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

const usage = `usage: washctl <command> [flags]

commands:
  plate [--json] <plate>                          show a plate's owner, plan, and washes
  quota-reset [--note text] <plate>               restart a Premium plate's monthly cap
  invoice [--split leasing] [-o file] <YYYY-MM>   fetch a month's fleet invoice as CSV
  health [--json]                                 show each site's sync state`

// environment is what every command needs: where to write, who to call, and how.
type environment struct {
	stdout, stderr io.Writer
	config         Config
	central        client
	invoicing      client
	site           client
}

type command func(ctx context.Context, args []string, env *environment) int

var commands = map[string]command{
	"plate":       runPlate,
	"quota-reset": runQuotaReset,
	"invoice":     runInvoice,
	"health":      runHealth,
}

// Run executes one washctl invocation and returns its exit code. Results go to stdout and problems to stderr,
// so a script can pipe the first without the second.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer, config Config) int {
	env := &environment{
		stdout:    stdout,
		stderr:    stderr,
		config:    config,
		central:   newClient(config.Timeout),
		invoicing: newClient(config.Timeout),
		site:      newClient(config.SiteTimeout),
	}
	if len(args) == 0 {
		return env.failUsage(usage)
	}
	run, isKnown := commands[args[0]]
	if !isKnown {
		return env.failUsage("unknown command %q\n\n%s", args[0], usage)
	}
	return run(ctx, args[1:], env)
}

func (e *environment) failUsage(format string, values ...any) int {
	_, _ = fmt.Fprintf(e.stderr, format+"\n", values...)
	return exitUsage
}

func (e *environment) fail(err error) int {
	_, _ = fmt.Fprintln(e.stderr, "washctl:", err)
	return exitFailure
}

// parseArgs parses flags wherever they sit among the positional arguments, since an operator types
// `plate ABC123 --json` as readily as `plate --json ABC123`, and the standard flag package stops at the first positional.
func parseArgs(flags *flag.FlagSet, args []string) ([]string, error) {
	flags.SetOutput(io.Discard)
	var positionals []string
	for {
		if err := flags.Parse(args); err != nil {
			return nil, err
		}
		if flags.NArg() == 0 {
			return positionals, nil
		}
		positionals = append(positionals, flags.Arg(0))
		args = flags.Args()[1:]
	}
}

// parseExactly parses args and requires exactly one positional argument, reporting a usage error otherwise.
func (e *environment) parseExactly(flags *flag.FlagSet, args []string, commandUsage string) (string, bool) {
	positionals, err := parseArgs(flags, args)
	if err != nil && !errors.Is(err, flag.ErrHelp) {
		_, _ = fmt.Fprintf(e.stderr, "%v\n", err)
	}
	if err != nil || len(positionals) != 1 {
		_, _ = fmt.Fprintln(e.stderr, commandUsage)
		return "", false
	}
	return positionals[0], true
}

// formatTime renders a moment the way every command prints it, and a missing one as never.
func formatTime(moment *time.Time) string {
	if moment == nil {
		return "never"
	}
	return moment.UTC().Format(time.RFC3339)
}
