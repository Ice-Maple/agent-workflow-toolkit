package bridge

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
)

// RunOwnerIfRequested is called by the public bridge command before normal
// flag parsing. A child owner never inherits the controller's stdin/stdout
// contract; its only durable interface is the private receipt and loopback
// endpoint.
func RunOwnerIfRequested(ctx context.Context, args []string) (bool, error) {
	if len(args) == 0 || args[0] != "--owner" {
		return false, nil
	}
	flags := flag.NewFlagSet("owner", flag.ContinueOnError)
	bootstrap := flags.String("owner-bootstrap", "", "private owner bootstrap path")
	if err := flags.Parse(args[1:]); err != nil {
		return true, err
	}
	if *bootstrap == "" {
		return true, errors.New("--owner-bootstrap is required")
	}
	if err := runOwnerProcess(*bootstrap); err != nil {
		return true, fmt.Errorf("bridge owner failed: %w", err)
	}
	return true, nil
}

func RunOwnerMain(ctx context.Context, args []string) int {
	requested, err := RunOwnerIfRequested(ctx, args)
	if !requested {
		return -1
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
