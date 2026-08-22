package layering_test

import (
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/strelov1/freehire/internal/arch/layering"
)

const modulePath = "github.com/strelov1/freehire"

// buildTags must name every constraint used in the repo. `go list` without them reports
// the untagged graph, and 222 files carry //go:build integration — including in-package
// tests in internal/db that import across blocks. A guard that cannot see them passes over
// the exact case CLAUDE.md warns about: green everywhere except CI, which runs the tagged
// suite. Adding the tags is safe here because the repo has no negated constraint and no
// legacy `// +build` line, so the tagged build is a strict superset.
const buildTags = "integration,llmlive"

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

	cmd := exec.CommandContext(t.Context(), "go", "list", "-tags", buildTags, "-f", format, modulePath+"/internal/...")
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			t.Fatalf("go list: %v\n%s", err, exit.Stderr)
		}
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

// atExpectedLocation reports whether a package sits at exactly the path Assignment gives
// it. This is stricter than "is under some block", and deliberately: a package the move
// script drops in the WRONG block is still under a block, and the layering check only
// catches it if the mistake happens to cross a layer. Two blocks a layer apart would hide
// it entirely.
//
// It also settles the pre-move case cleanly. internal/job and internal/search are packages
// whose names collide with block names, so "is the head segment a block name" would call
// a hypothetical internal/search/foo moved when nothing has moved.
func atExpectedLocation(importPath string) bool {
	name := relName(importPath)
	block, ok := layering.Assignment[name]
	if !ok {
		return false
	}
	return importPath == modulePath+"/internal/"+block+"/"+name
}

func TestEveryPackageSitsWhereAssignmentSaysItShould(t *testing.T) {
	graph := repoGraph(t)

	placed, misplaced := 0, []string(nil)
	for importPath := range graph {
		if atExpectedLocation(importPath) {
			placed++
			continue
		}
		misplaced = append(misplaced, importPath)
	}
	if placed == 0 {
		t.Skipf("the move has not started: none of the %d packages is at its assigned path", len(graph))
	}
	slices.Sort(misplaced)
	for _, importPath := range misplaced {
		name := relName(importPath)
		t.Errorf("%s is not where it belongs: Assignment puts %q in block %q",
			importPath, name, layering.Assignment[name])
	}
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
// here fails too. So creating silence (task 2.4) without deleting
// its line is a test failure, and the list cannot quietly outlive its purpose.
var pendingExtraction []string

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
		// An entry in neither the repo nor the block table excuses nothing and would sit
		// here forever unnoticed.
		if _, assigned := layering.Assignment[name]; !assigned {
			t.Errorf("pendingExtraction names %q, which the block table does not place", name)
		}
	}
}

// remapped rewrites every real import path into the path it will have after the move, by
// looking each package up in Assignment. It lets the layering be evaluated against the
// current tree, so the prerequisite extractions are a red-to-green ratchet rather than a
// plan that is only checked once the move has already happened.
func remapped(t *testing.T) map[string][]string {
	t.Helper()

	move := func(importPath string) string {
		name := relName(importPath)
		if name == "" {
			return importPath
		}
		block, ok := layering.Assignment[name]
		if !ok {
			return importPath
		}
		return modulePath + "/internal/" + block + "/" + name
	}

	out := make(map[string][]string)
	for pkg, imports := range repoGraph(t) {
		moved := make([]string, 0, len(imports))
		for _, imp := range imports {
			moved = append(moved, move(imp))
		}
		out[move(pkg)] = moved
	}
	return out
}

// plannedViolations are the upward edges still awaiting a prerequisite extraction, each
// tagged with the task that removes it. Empty: every one has landed, so the block table
// now describes a graph that already layers cleanly, before a single directory has moved. The test below asserts the post-move graph has
// EXACTLY these and no others, in both directions: a new upward edge fails, and an edge
// that has been fixed without being struck off this list fails too.
var plannedViolations = map[string]string{}

func TestPostMoveGraphHasOnlyThePlannedViolations(t *testing.T) {
	seen := map[string]bool{}
	for _, v := range layering.Check(remapped(t), layering.Layers) {
		key := fmt.Sprintf("%s -> %s", relToBlock(v.From), relToBlock(v.To))
		seen[key] = true
		if _, planned := plannedViolations[key]; !planned {
			t.Errorf("unplanned layering violation: %s (layer %d -> %d)", key, v.FromLayer, v.ToLayer)
		}
	}
	for key, task := range plannedViolations {
		if !seen[key] {
			t.Errorf("%q no longer violates — task %s is done, strike it off plannedViolations", key, task)
		}
	}
}

// relToBlock renders a post-move import path as "<block>/<pkg>", which is how the design
// and the task list name these edges.
func relToBlock(importPath string) string {
	rest, _ := strings.CutPrefix(importPath, modulePath+"/internal/")
	return rest
}

// The repo-wide assertion, and the thing task 3.3 turns on. It skips only while the move
// has not started at all — every internal package still directly under internal/, so every
// one reports as unassigned. The moment a single package lands in a block, this test starts
// failing on anything that is wrong, so it cannot be forgotten.
func TestRepoRespectsTheLayering(t *testing.T) {
	graph := repoGraph(t)

	// "Not started" means no package sits at its assigned path yet. Counting unassigned
	// packages instead would not do: a nested package like internal/auth/apple parses its
	// parent as a block name, so it reports unknown-block rather than unassigned.
	placed := 0
	for importPath := range graph {
		if atExpectedLocation(importPath) {
			placed++
		}
	}
	if placed == 0 {
		t.Skipf("the move has not started: none of the %d packages is at its assigned path", len(graph))
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
