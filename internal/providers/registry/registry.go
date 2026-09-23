// Package registry provides the ordered list of all providers.
// Order matters: the first provider whose Detect() returns true wins.
// This follows Railpack's convention where more specific providers
// are checked before generic ones (e.g., PHP before Node, Static last).
package registry

import (
	"github.com/idlistack/cli/internal/provider"
	"github.com/idlistack/cli/internal/providers/deno"
	"github.com/idlistack/cli/internal/providers/dotnet"
	"github.com/idlistack/cli/internal/providers/elixir"
	"github.com/idlistack/cli/internal/providers/golang"
	"github.com/idlistack/cli/internal/providers/java"
	"github.com/idlistack/cli/internal/providers/node"
	"github.com/idlistack/cli/internal/providers/php"
	"github.com/idlistack/cli/internal/providers/python"
	"github.com/idlistack/cli/internal/providers/ruby"
	"github.com/idlistack/cli/internal/providers/rust"
	"github.com/idlistack/cli/internal/providers/shell"
	"github.com/idlistack/cli/internal/providers/staticfile"
	"github.com/idlistack/cli/internal/providers/whatomate"
)

// GetProviders returns all language providers in detection order.
// Order is critical — first match wins. Rationale:
//
//   1. PHP      — A PHP project with package.json (for frontend) must be detected as PHP
//   2. Go       — go.mod is unambiguous
//   3. Java     — pom.xml / gradlew are unambiguous
//   4. Rust     — Cargo.toml is unambiguous
//   5. Ruby     — Gemfile is unambiguous
//   6. Elixir   — mix.exs is unambiguous
//   7. Python   — requirements.txt could coexist, but checked before Node
//   8. Deno     — deno.json before Node (Deno projects may also have package.json)
//   9. .NET     — *.csproj is unambiguous
//   10. Node    — package.json is very common, checked last among languages
//   11. Static  — index.html with no other manifests
//   12. Shell   — Universal fallback
func GetProviders() []provider.Provider {
	return []provider.Provider{
		&whatomate.WhatomateProvider{},
		&php.PhpProvider{},
		&golang.GoProvider{},
		&java.JavaProvider{},
		&rust.RustProvider{},
		&ruby.RubyProvider{},
		&elixir.ElixirProvider{},
		&python.PythonProvider{},
		&deno.DenoProvider{},
		&dotnet.DotnetProvider{},
		&node.NodeProvider{},
		&staticfile.StaticfileProvider{},
		&shell.ShellProvider{},
	}
}

// GetProvider returns a specific provider by name, or nil if not found.
func GetProvider(name string) provider.Provider {
	for _, p := range GetProviders() {
		if p.Name() == name {
			return p
		}
	}
	return nil
}
