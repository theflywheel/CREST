// Command structure enforces the repository layout described in docs/COMPONENTS.md.
//
// golangci-lint enforces import boundaries; this enforces the shape those
// boundaries assume. Both exist because a layout rule that lives only in a
// document is one that gets broken in month three, when the person who wrote
// the document is busy.
package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type violation struct {
	path string
	why  string
}

// Top-level directories that are allowed to exist, and what each is for.
var allowedTop = map[string]string{
	"schemas":   "JSON Schema — source of truth for Go and TypeScript",
	"services":  "crest-core (the infrastructure service) and the payments application",
	"pkg":       "shared Go libraries",
	"adapters":  "source-system adapters",
	"cmd":       "operator and harness CLIs",
	"apps":      "the static-era frontends (shrinking as doors move to frontend/, #153)",
	"frontend":  "the pnpm workspace: rebuilt doors and their shared packages (#153)",
	"harness":   "E2E scenarios and the W1-W10 suite",
	"infra":     "compose and deployment config",
	"tools":     "mocks, codegen, this linter",
	"tests":     "contract tests",
	"docs":      "design documents",
	"reference": "source material the design came from",
}

// Directories that exist on disk but are not part of the repository's layout.
var ignoredDir = map[string]bool{
	"node_modules": true,
	"vendor":       true,
}

// Exactly the services named in the blueprint. A new top-level service is a
// design decision, not a directory someone creates on a Tuesday.
// core is one deployable holding four member packages (#150) — the members
// are its subdirectories, not top-level services.
var knownServices = map[string]bool{
	"core": true, "payments": true,
}

func main() {
	root, err := repoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "structure:", err)
		os.Exit(1)
	}

	var vs []violation
	vs = append(vs, checkTopLevel(root)...)
	vs = append(vs, checkServices(root)...)
	vs = append(vs, checkNoStrayGoFiles(root)...)

	if len(vs) == 0 {
		fmt.Println("structure: ok")
		return
	}
	fmt.Fprintf(os.Stderr, "structure: %d violation(s)\n\n", len(vs))
	for _, v := range vs {
		fmt.Fprintf(os.Stderr, "  %s\n    %s\n", v.path, v.why)
	}
	fmt.Fprintln(os.Stderr, "\nLayout is documented in docs/COMPONENTS.md. If the layout should")
	fmt.Fprintln(os.Stderr, "change, change it there and here deliberately — do not work around it.")
	os.Exit(1)
}

func repoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for dir := wd; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		if dir == filepath.Dir(dir) {
			return "", fmt.Errorf("no go.mod found above %s", wd)
		}
	}
}

func checkTopLevel(root string) []violation {
	entries, err := os.ReadDir(root)
	if err != nil {
		return []violation{{root, err.Error()}}
	}
	var vs []violation
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}
		// Dependency trees are not layout. They are gitignored, but this
		// linter walks the filesystem rather than the index, so it has to be
		// told — otherwise a `pnpm install` makes `make structure` fail.
		if ignoredDir[name] {
			continue
		}
		if _, ok := allowedTop[name]; !ok {
			vs = append(vs, violation{
				path: name + "/",
				why: "unexpected top-level directory. Allowed: " +
					strings.Join(sortedKeys(allowedTop), ", "),
			})
		}
	}
	return vs
}

func checkServices(root string) []violation {
	dir := filepath.Join(root, "services")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var vs []violation
	for _, e := range entries {
		if !e.IsDir() {
			vs = append(vs, violation{
				path: filepath.Join("services", e.Name()),
				why:  "services/ holds one directory per service, not loose files",
			})
			continue
		}
		if !knownServices[e.Name()] {
			vs = append(vs, violation{
				path: filepath.Join("services", e.Name()) + "/",
				why: "unknown service. The services are fixed by the blueprint (§13): " +
					strings.Join(sortedKeys(toMap(knownServices)), ", ") +
					". Adding one is a design decision — record it in the blueprint first.",
			})
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, e.Name(), "main.go")); err != nil {
			vs = append(vs, violation{
				path: filepath.Join("services", e.Name()),
				why:  "every service needs a main.go entrypoint",
			})
		}
	}
	return vs
}

// Go source belongs in a known Go tree. A .go file under docs/ or apps/ means
// something has drifted.
func checkNoStrayGoFiles(root string) []violation {
	goTrees := []string{"pkg", "services", "adapters", "cmd", "harness", "tools", "tests"}
	var vs []violation

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // an unreadable path is not a layout violation
		}
		name := d.Name()
		if d.IsDir() {
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "reference" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil //nolint:nilerr // as above
		}
		top := strings.SplitN(rel, string(filepath.Separator), 2)[0]
		if top == rel {
			return nil // a .go file at the repo root is caught by review, not layout
		}
		for _, t := range goTrees {
			if top == t {
				return nil
			}
		}
		vs = append(vs, violation{
			path: rel,
			why:  "Go source outside a Go tree (" + strings.Join(goTrees, ", ") + ")",
		})
		return nil
	})
	if err != nil {
		vs = append(vs, violation{".", err.Error()})
	}
	return vs
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func toMap(m map[string]bool) map[string]string {
	out := make(map[string]string, len(m))
	for k := range m {
		out[k] = ""
	}
	return out
}
