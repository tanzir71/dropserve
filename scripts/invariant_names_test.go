package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestProductInvariantTestsKeepTheirSpecifiedNames(t *testing.T) {
	t.Parallel()
	wanted := map[string]bool{
		"TestDropFolderBecomesReachable":        false,
		"TestAppFolderIsReadOnlyToUs":           false,
		"TestAdvertisedURLsAllRespond":          false,
		"TestOneBrokenAppDoesNotAffectOthers":   false,
		"TestNoImplicitPublicExposure":          false,
		"TestTrustStoreRequiresExplicitConsent": false,
	}
	fileSet := token.NewFileSet()
	err := filepath.WalkDir("..", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == "dist") {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		parsed, parseErr := parser.ParseFile(fileSet, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if _, required := wanted[function.Name.Name]; required {
				wanted[function.Name.Name] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan invariant tests: %v", err)
	}
	missing := make([]string, 0, len(wanted))
	for name, found := range wanted {
		if !found {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) != 0 {
		t.Fatalf("specified product-invariant tests are missing: %s", strings.Join(missing, ", "))
	}
}
