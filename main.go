package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"

	"ncloud-api/server"
)

// run is the main function to run the program. creates context and runs the server
func run(ctx context.Context, w io.Writer, args []string) error {
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt)
	defer cancel()

	return server.Run(ctx, w, args)
}

func main() {
	ctx := context.Background()
	if err := run(ctx, os.Stdout, os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
	}
}
