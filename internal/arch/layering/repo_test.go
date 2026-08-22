package layering_test

import (
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/strelov1/freehire/internal/arch/layering"
)

const modulePath = "github.com/strelov1/freehire"

// repoGraph shells out to `go list` for the real import graph of internal/. Test imports
// are included deliberately: a _test.go file can create a cross-layer dependency the
// production build never reveals, and the analysis this layering came from counted them.
func repoGraph(t *testing.T) map[string][]string {
	t.Helper()

	const sep = "\x1f"
	format := "{{.ImportPath}}" + sep +
		"{{join .Imports \",\"}}" + sep +
		"{{join .TestImports \",\"}}" + sep +
		"{{join .XTestImports \",\"}}"

	out, err := exec.CommandContext(t.Context(), "go", "list", "-f", format, modulePath+"/internal/...").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}

	graph := make(map[string][]string)
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		parts := strings.Split(line, sep)
		if len(parts) != 4 {
			t.Fatalf("unparseable go list line: %q", line)
		}
		var imports []string
		for _, group := range parts[1:] {
			for _, imp := range strings.Split(group, ",") {
				if imp != "" {
					imports = append(imports, imp)
				}
			}
		}
		graph[parts[0]] = imports
	}
	if len(graph) == 0 {
		t.Fatal("go list returned no packages; the guard would pass vacuously")
	}
	return graph
}

// relName strips the module and internal/ prefixes, and the block segment when the package
// already sits in a block. It therefore names a package the same way before and after the
// move, which is what lets this test assert coverage throughout.
func relName(importPath string) string {
	rest, ok := strings.CutPrefix(importPath, modulePath+"/internal/")
	if !ok {
		return ""
	}
	if head, tail, found := strings.Cut(rest, "/"); found {
		if _, isBlock := layering.Layers[head]; isBlock {
			return tail
		}
	}
	return rest
}

// inKnownBlock reports whether a package already sits under one of the eleven blocks.
func inKnownBlock(importPath string) bool {
	rest, ok := strings.CutPrefix(importPath, modulePath+"/internal/")
	if !ok {
		return false
	}
	head, _, found := strings.Cut(rest, "/")
	if !found {
		return false
	}
	_, isBlock := layering.Layers[head]
	return isBlock
}

func TestEveryPackageInTheRepoIsAssignedToABlock(t *testing.T) {
	var missing []string
	for importPath := range repoGraph(t) {
		name := relName(importPath)
		if _, ok := layering.Assignment[name]; !ok {
			missing = append(missing, name)
		}
	}
	slices.Sort(missing)
	if len(missing) > 0 {
		t.Errorf("packages exist in the repo but are in no block: %v", missing)
	}
}

// pendingExtraction names the packages the block table already places but the prerequisite
// extractions have not created yet. It is a ratchet in both directions: a package missing
// from the repo AND absent here fails, and a package that now exists but is still listed
// here fails too. So creating provider (task 2.2) or silence (task 2.4) without deleting
// its line is a test failure, and the list cannot quietly outlive its purpose.
var pendingExtraction = []string{"provider", "silence"}

func TestBlockTableNamesNoPackageThatDoesNotExist(t *testing.T) {
	present := make(map[string]bool)
	for importPath := range repoGraph(t) {
		present[relName(importPath)] = true
	}
	var stale []string
	for name := range layering.Assignment {
		if !present[name] && !slices.Contains(pendingExtraction, name) {
			stale = append(stale, name)
		}
	}
	slices.Sort(stale)
	if len(stale) > 0 {
		t.Errorf("block table names packages that do not exist: %v", stale)
	}
	for _, name := range pendingExtraction {
		if present[name] {
			t.Errorf("%q now exists; remove it from pendingExtraction", name)
		}
	}
}

// The repo-wide assertion, and the thing task 3.3 turns on. It skips only while the move
// has not started at all — every internal package still directly under internal/, so every
// one reports as unassigned. The moment a single package lands in a block, this test starts
// failing on anything that is wrong, so it cannot be forgotten.
func TestRepoRespectsTheLayering(t *testing.T) {
	graph := repoGraph(t)

	// "Not started" means no package sits in a recognized block yet. Counting unassigned
	// packages instead would not do: a nested package like internal/auth/apple parses its
	// parent as a block name, so it reports unknown-block rather than unassigned.
	inBlocks := 0
	for importPath := range graph {
		if inKnownBlock(importPath) {
			inBlocks++
		}
	}
	if inBlocks == 0 {
		t.Skipf("the move has not started: none of the %d packages is in a block yet", len(graph))
	}

	violations := layering.Check(graph, layering.Layers)
	if len(violations) == 0 {
		return
	}
	var b strings.Builder
	for _, v := range violations {
		b.WriteString("\n  " + v.String())
	}
	t.Errorf("%d layering violations:%s", len(violations), b.String())
}
