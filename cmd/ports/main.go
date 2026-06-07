// Command ports lists and manages the dev servers and databases you have
// running on local ports, mapping each back to the repo it was launched from.
package main

import "github.com/joshmcadams/ports/internal/cli"

func main() {
	cli.Execute()
}
