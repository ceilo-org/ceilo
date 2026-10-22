package cli

// commands lists every subcommand. To add one, write it in its own file and
// add it here.
func commands(a *App) []Command {
	return []Command{
		&runCmd{},
		&helpCmd{app: a}, // help needs the App to list the other commands
	}
}
