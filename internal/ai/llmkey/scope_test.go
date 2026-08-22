package llmkey

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/strelov1/freehire/internal/platform/modroot"
)

// Work that belongs to nobody must never be attributed to somebody. A catalogue vacancy
// has no owner, and enriching it under whichever account happened to trigger a run would
// put a cost on a person who did not incur it — a lie in exactly the report this package
// exists to make truthful.
//
// The rule is easiest to break by accident and impossible to notice afterwards: the spend
// lands on a real account and looks like ordinary use. So it is enforced structurally —
// a background entry point that reaches for a per-user credential fails here, at compile
// time of the test, rather than in a report nobody audits.
//
// cmd/server is the one binary that may: it serves people.
func TestBackgroundEntryPointsResolveNoUserCredential(t *testing.T) {
	const self = "github.com/strelov1/freehire/internal/ai/llmkey"

	// Relative to the module root, found by walking up to go.mod, rather than by counting
	// "../" from this file. The block move buried this package one level deeper and a
	// hardcoded depth simply stopped finding cmd/ — an lstat error here, but one directory
	// higher it would have walked an empty tree and passed while checking nothing.
	root := moduleRoot(t)
	roots := map[string]string{
		"cmd":                               filepath.Join(root, "cmd"),
		"internal/ai/enrich":                filepath.Join(root, "internal", "ai", "enrich"),
		"internal/ingest/telegram":          filepath.Join(root, "internal", "ingest", "telegram"),
		"internal/application/mailclassify": filepath.Join(root, "internal", "application", "mailclassify"),
		"internal/ai/embed":                 filepath.Join(root, "internal", "ai", "embed"),
	}
	for label, dir := range roots {
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("%s: %v — the guard would walk nothing and pass vacuously", label, err)
		}
	}

	for label, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			// The one binary that serves people is allowed to name them.
			if strings.Contains(filepath.ToSlash(path), "/cmd/server/") {
				return nil
			}
			file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if parseErr != nil {
				return parseErr
			}
			for _, imp := range file.Imports {
				if name, unquoteErr := strconv.Unquote(imp.Path.Value); unquoteErr == nil && name == self {
					t.Errorf("%s imports %s; background work has no owner and must spend on the "+
						"service credential", path, self)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", label, err)
		}
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	root, err := modroot.Find()
	if err != nil {
		t.Fatal(err)
	}
	return root
}
