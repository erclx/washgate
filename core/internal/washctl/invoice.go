package washctl

import (
	"context"
	"flag"
	"os"
	"regexp"
)

const (
	invoiceUsage = "usage: washctl invoice [--split leasing] [-o file] <YYYY-MM>"
	splitLeasing = "leasing"
)

var invoiceMonth = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)

func runInvoice(ctx context.Context, args []string, env *environment) int {
	flags := flag.NewFlagSet("invoice", flag.ContinueOnError)
	split := flags.String("split", "", "split the invoice by leasing company")
	outputPath := flags.String("o", "", "write the CSV to this file instead of stdout")
	month, isValid := env.parseExactly(flags, args, invoiceUsage)
	if !isValid {
		return exitUsage
	}
	if !invoiceMonth.MatchString(month) {
		return env.failUsage("the month must look like YYYY-MM, got %q", month)
	}
	if *split != "" && *split != splitLeasing {
		return env.failUsage("--split takes %q, the only split there is, got %q", splitLeasing, *split)
	}

	target := env.config.InvoicingURL + "/invoices/" + month + ".csv"
	if *split != "" {
		target += "?split=" + *split
	}
	// The whole answer is read before anything is written, so a failure leaves stdout and the file untouched.
	answer, err := env.invoicing.get(ctx, target)
	if err != nil {
		return env.fail(err)
	}
	if *outputPath == "" {
		_, _ = env.stdout.Write(answer.body)
		return exitOK
	}
	if err := os.WriteFile(*outputPath, answer.body, 0o600); err != nil {
		return env.fail(err)
	}
	return exitOK
}
