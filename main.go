package main

import (
	"context"
	"fmt"
	"github.com/beyondan/gqlgen/api"
	"github.com/beyondan/gqlgenc/clientgen"
	"github.com/beyondan/gqlgenc/config"
	"github.com/beyondan/gqlgenc/generator"
	"os"
)

func main() {
	ctx := context.Background()
	cfg, err := config.LoadConfigFromDefaultLocations()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cfg: %+v", err.Error())
		os.Exit(2)
	}

	clientGen := api.AddPlugin(clientgen.New(cfg))

	if err := generator.Generate(ctx, cfg, clientGen); err != nil {
		fmt.Fprintf(os.Stderr, "generate: %+v", err.Error())
		os.Exit(4)
	}
}
