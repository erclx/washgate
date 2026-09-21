// Command washctl is the operations CLI for looking up plates, quotas, and invoices.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/erclx/washgate/core/internal/washctl"
)

func main() {
	config, err := washctl.LoadConfig(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "washctl:", err)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := washctl.Run(ctx, os.Args[1:], os.Stdout, os.Stderr, config)
	stop()
	os.Exit(code)
}
