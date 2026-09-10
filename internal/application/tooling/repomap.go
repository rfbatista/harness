package tooling

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"operators-mcp/internal/domain"
)

// RepoMapTools returns the repo_map tool for providing compact codebase overviews.
func RepoMapTools() []domain.Tool {
	return []domain.Tool{
		repoMapTool(),
	}
}

func repoMapTool() domain.Tool {
	return domain.Tool{
		Name: "repo_map",
		Description: `Generate a compact structural overview of the codebase.

Uses tree-sitter to extract symbol definitions (functions, types, classes, methods,
interfaces) from source files. Returns a condensed map showing what each file defines,
helping navigate the codebase without reading every file.

Supports Go, JavaScript, TypeScript, and Python files.`,
		InputSchema: schemaFromJSON(`{
			"type": "object",
			"properties": {
				"path": {
					"type": "string",
					"description": "Root directory to scan (default '.')."
				},
				"query": {
					"type": "string",
					"description": "Focus the map on files and symbols matching this query."
				},
				"max_lines": {
					"type": "integer",
					"description": "Maximum output lines (default 200)."
				},
				"max_depth": {
					"type": "integer",
					"description": "Maximum directory depth to scan (default 5)."
				}
			}
		}`),
		Source: "code",
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			root := getString(args, "path", ".")
			if root == "" {
				root = "."
			}
			query := getString(args, "query", "")
			maxLines := getInt(args, "max_lines", 200)
			maxDepth := getInt(args, "max_depth", 5)

			entries, err := buildRepoMap(root, query, maxDepth)
			if err != nil {
				return nil, &domain.StructuredError{Code: "REPO_MAP_ERROR", Message: err.Error()}
			}

			output := formatRepoMap(entries, maxLines)

			totalSymbols := 0
			for _, e := range entries {
				totalSymbols += len(e.symbols)
			}

			return map[string]any{
				"map":           output,
				"files_scanned": len(entries),
				"total_symbols": totalSymbols,
			}, nil
		},
	}
}

type repoMapEntry struct {
	path      string
	symbols   []Symbol
	relevance float64
}

func buildRepoMap(root, query string, maxDepth int) ([]repoMapEntry, error) {
	gitignorePatterns := parseGitignore(root)

	var entries []repoMapEntry

	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		relPath, err := filepath.Rel(root, p)
		if err != nil || relPath == "." {
			return nil
		}

		depth := strings.Count(relPath, string(filepath.Separator))
		if depth >= maxDepth {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		isDir := info.IsDir()
		if isIgnored(relPath, isDir, gitignorePatterns) {
			if isDir {
				return filepath.SkipDir
			}
			return nil
		}

		if isDir {
			return nil
		}

		ext := filepath.Ext(relPath)
		if !TSSupportsExt(ext) {
			return nil
		}

		if info.Size() > 512*1024 {
			return nil
		}

		data, err := os.ReadFile(p)
		if err != nil {
			return nil
		}

		symbols := TSExtractSymbols(data, relPath)
		if len(symbols) == 0 {
			return nil
		}

		rel := 1.0
		if query != "" {
			rel = computeRelevance(relPath, symbols, query)
		}

		entries = append(entries, repoMapEntry{
			path:      relPath,
			symbols:   symbols,
			relevance: rel,
		})
		return nil
	})

	if err != nil {
		return nil, err
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].relevance != entries[j].relevance {
			return entries[i].relevance > entries[j].relevance
		}
		return entries[i].path < entries[j].path
	})

	return entries, nil
}

func computeRelevance(path string, symbols []Symbol, query string) float64 {
	q := strings.ToLower(query)
	terms := strings.Fields(q)

	score := 0.0
	pathLower := strings.ToLower(path)
	for _, t := range terms {
		if strings.Contains(pathLower, t) {
			score += 2.0
		}
	}

	for _, sym := range symbols {
		nameLower := strings.ToLower(sym.Name)
		for _, t := range terms {
			if strings.Contains(nameLower, t) {
				score += 1.0
			}
		}
	}

	if score == 0 {
		score = 0.01
	}
	return score
}

func formatRepoMap(entries []repoMapEntry, maxLines int) string {
	var b strings.Builder
	lineCount := 0

	for _, e := range entries {
		if lineCount >= maxLines {
			break
		}

		fmt.Fprintf(&b, "%s:\n", e.path)
		lineCount++

		for _, sym := range e.symbols {
			if lineCount >= maxLines {
				fmt.Fprintf(&b, "│ ... (%d more symbols)\n", len(e.symbols))
				lineCount++
				break
			}

			sig := sym.Signature
			if sig == "" {
				sig = sym.Name
			}
			fmt.Fprintf(&b, "│ %s %s\n", sym.Kind, sig)
			lineCount++
		}

		b.WriteString("\n")
		lineCount++
	}

	return strings.TrimSpace(b.String())
}
