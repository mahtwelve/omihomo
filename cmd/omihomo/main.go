// Command omihomo is the omihomo daemon and the owner's command line client.
package main

import (
	"context"
	"os"

	"github.com/readeem/omihomo/internal/cli"
)

func main() {
	os.Exit(cli.Main(context.Background()))
}
