package main

import (
	"os"

	"github.com/raychang/ai-usage-bar/internal/app"
)

func main() {
	os.Exit(app.New().Run(os.Args[1:]))
}
