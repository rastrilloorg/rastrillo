package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"amadan.net/rastrillo/rastrillo/internal/routelist"
)

// runRoutes prints the app's route table, and with --check fails an
// action no route mounts. A signal stops the listing, which cleans up,
// and then ends the command as the signal would have.
func runRoutes(args []string, out io.Writer) error {
	chosen, check, dir, err := routesArguments(args)
	if err != nil {
		return err
	}
	ctx, stop := routelist.Interruptible()
	defer stop()
	err = listRoutes(ctx, chosen, check, dir, out)
	if received := routelist.Interruption(ctx); received != nil &&
		err != nil {
		fmt.Fprintf(os.Stderr, "rastrillo: %v\n", err)
		routelist.EndBy(received)
	}
	return err
}

func listRoutes(
	ctx context.Context, chosen string, check bool, dir string,
	out io.Writer,
) error {
	target, err := routelist.Find(ctx, dir, chosen)
	if err != nil {
		return err
	}
	routes, err := routelist.List(ctx, target)
	if err != nil {
		return err
	}
	fmt.Fprint(out, routelist.Format(routes))
	if !check {
		return nil
	}
	unmounted, err := routelist.Unmounted(ctx, target, routes)
	if err != nil {
		return err
	}
	if len(unmounted) > 0 {
		return exitError{code: 1, msg: routelist.FormatUnmounted(unmounted)}
	}
	return nil
}

// routesArguments reads [--package path] [--check] [dir]. A FlagSet,
// because the CLI page's check reads its declarations.
func routesArguments(args []string) (
	chosen string, check bool, dir string, err error) {
	flags := flag.NewFlagSet("routes", flag.ContinueOnError)
	flags.SetOutput(io.Discard) // the usage line below says it once
	flags.StringVar(&chosen, "package", "", "the package whose Routes to call")
	flags.BoolVar(&check, "check", false, "fail an action no route mounts")
	if err := flags.Parse(args); err != nil || flags.NArg() > 1 {
		return "", false, "", exitError{code: 2, msg: routelist.Usage}
	}
	dir = "."
	if flags.NArg() == 1 {
		dir = flags.Arg(0)
	}
	return chosen, check, dir, nil
}
