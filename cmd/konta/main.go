package main

import (
	"os"

	"github.com/talyguryn/konta/internal/app"
)

const Version = "0.3.40"

func main() {
	os.Exit(app.New(Version).Run(os.Args[1:]))
}
