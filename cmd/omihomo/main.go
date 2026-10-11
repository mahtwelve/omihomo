// Command omihomo is the omihomo daemon and the owner's command line client.
package main

import (
	"context"
	"os"

	"github.com/readeem/omihomo/internal/cli"
)

func main() {
	os.Exit(cli.Run(context.Background(), os.Args[1:], cli.Env{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}))
}
