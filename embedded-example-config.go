// Package claudebridge holds files shared by the commands: the example
// configuration the macOS app writes into its config folder on first launch.
package claudebridge

import _ "embed"

// ExampleEnv is .env.example.
//
//go:embed .env.example
var ExampleEnv []byte

// ExampleProjects is projects.example.json.
//
//go:embed projects.example.json
var ExampleProjects []byte
