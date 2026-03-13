package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/routerr/aubar/internal/geminiquota"
)

func main() {
	flagToken := flag.String("token", "", "OAuth Access Token (overrides ~/.gemini/oauth_creds.json)")
	flag.Parse()

	out := geminiquota.Collect(context.Background(), geminiquota.Options{
		Token: *flagToken,
	})
	if err := geminiquota.PrintJSON(os.Stdout, out); err != nil {
		fmt.Fprintf(os.Stderr, "Error encoding output: %v\n", err)
		os.Exit(1)
	}
}
