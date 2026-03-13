package main

import (
	"os"

	"github.com/routerr/aubar/internal/app"
)

func main() {
	os.Exit(app.New().Run(os.Args[1:]))
}
