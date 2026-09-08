package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/MiguelRodo/github-projects-skill/internal/cli"
	"github.com/MiguelRodo/github-projects-skill/internal/githubcli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(cli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr, githubcli.ExecRunner{}))
}
