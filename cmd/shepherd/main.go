package main

import (
	"os"

	"github.com/JacobRWebb/shepherd/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
