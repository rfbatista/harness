package tooling

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"operators-mcp/internal/domain"
)

// FilesystemTools returns tools for reading, listing, editing, and writing files.
func FilesystemTools() []domain.Tool {
	return []domain.Tool{
		readFileTool(),
		listFilesTool(),
		editFileTool(),
		writeFileTool(),
	}
}

// ---------------------------------------------------------------------------
// read_file
// ---------------------------------------------------------------------------

func readFileTool() domain.Tool {
	return domain.Tool{
		Name: "read_file",
		Description: `Read the contents of a file with line numbers.

Returns each line as LINE_NUM|LINE_CONTENT (e.g. "  42|func main() {").
Supports partial reads via start_line/end_line and truncation via max_lines.`,
		InputSchema: schemaFromJSON(`{
			"type": "object",
			"properties": {
				"path": {
					"type": "string",
					"description": "The relative path of a file in the working directory."
				},
				"start_line": {
					"type": "integer",
					"description": "First line to return (1-based, optional)."
				},
				"end_line": {
					"type": "integer",
					"description": "Last line to return (1-based, optional)."
				},
				"max_lines": {
					"type": "integer",
					"description": "Maximum lines to return (default 500)."
				}
			},
			"required": ["path"]
		}`),
		Source: "code",
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			p := getString(args, "path", "")
			if p == "" {
				return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "path is required"}
			}

			startLine := getInt(args, "start_line", 0)
			endLine := getInt(args, "end_line", 0)
			maxLines := getInt(args, "max_lines", 500)

			data, err := os.ReadFile(p)
			if err != nil {
				return nil, &domain.StructuredError{Code: "READ_FILE_ERROR", Message: err.Error()}
			}

			allLines := strings.Split(string(data), "\n")
			totalLines := len(allLines)

			if startLine <= 0 {
				startLine = 1
			}
			if endLine <= 0 || endLine > totalLines {
				endLine = totalLines
			}
			if startLine > endLine {
				startLine = endLine
			}

			selected := allLines[startLine-1 : endLine]

			truncated := false
			if maxLines > 0 && len(selected) > maxLines {
				selected = selected[:maxLines]
				endLine = startLine + maxLines - 1
				truncated = true
			}

			width := len(fmt.Sprintf("%d", endLine))
			var buf strings.Builder
			for i, line := range selected {
				lineNum := startLine + i
				fmt.Fprintf(&buf, "%*d|%s\n", width, lineNum, line)
			}

			result := map[string]any{
				"content":        buf.String(),
				"total_lines":    totalLines,
				"returned_range": []int{startLine, startLine + len(selected) - 1},
				"language":       inferLanguage(p),
			}
			if truncated {
				result["truncated"] = fmt.Sprintf("... truncated, %d total lines", totalLines)
			}
			return result, nil
		},
	}
}

// ---------------------------------------------------------------------------
// list_files
// ---------------------------------------------------------------------------

func listFilesTool() domain.Tool {
	return domain.Tool{
		Name: "list_files",
		Description: `List files and directories at a given path.

Respects .gitignore by default, supports depth limiting, glob patterns,
and returns structured metadata (path, type, size).`,
		InputSchema: schemaFromJSON(`{
			"type": "object",
			"properties": {
				"path": {
					"type": "string",
					"description": "Directory to list (default '.')."
				},
				"max_depth": {
					"type": "integer",
					"description": "Maximum directory depth (default 3)."
				},
				"include_pattern": {
					"type": "string",
					"description": "Glob pattern to include (e.g. '*.go')."
				},
				"exclude_pattern": {
					"type": "string",
					"description": "Glob pattern to exclude."
				},
				"respect_gitignore": {
					"type": "boolean",
					"description": "Whether to respect .gitignore (default true)."
				}
			}
		}`),
		Source: "code",
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			dir := getString(args, "path", ".")
			if dir == "" {
				dir = "."
			}
			maxDepth := getInt(args, "max_depth", 3)
			includePattern := getString(args, "include_pattern", "")
			excludePattern := getString(args, "exclude_pattern", "")
			respectGitignore := getBool(args, "respect_gitignore", true)

			const maxEntries = 1000

			var patterns []gitignorePattern
			if respectGitignore {
				patterns = parseGitignore(dir)
			}

			var entries []map[string]any
			err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
				if err != nil {
					return nil // skip unreadable entries
				}

				relPath, err := filepath.Rel(dir, p)
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

				if respectGitignore && isIgnored(relPath, isDir, patterns) {
					if isDir {
						return filepath.SkipDir
					}
					return nil
				}

				if excludePattern != "" {
					if m, _ := filepath.Match(excludePattern, filepath.Base(p)); m {
						if isDir {
							return filepath.SkipDir
						}
						return nil
					}
				}

				if includePattern != "" && !isDir {
					if m, _ := filepath.Match(includePattern, filepath.Base(p)); !m {
						return nil
					}
				}

				entryType := "file"
				if isDir {
					entryType = "dir"
				}

				entries = append(entries, map[string]any{
					"path": relPath,
					"type": entryType,
					"size": info.Size(),
				})

				if len(entries) >= maxEntries {
					return fmt.Errorf("__limit_reached__")
				}
				return nil
			})

			truncated := false
			if err != nil && err.Error() == "__limit_reached__" {
				truncated = true
				err = nil
			}
			if err != nil {
				return nil, &domain.StructuredError{Code: "LIST_FILES_ERROR", Message: err.Error()}
			}

			result := map[string]any{
				"entries": entries,
				"count":   len(entries),
			}
			if truncated {
				result["truncated"] = fmt.Sprintf("results capped at %d entries", maxEntries)
			}
			return result, nil
		},
	}
}

// ---------------------------------------------------------------------------
// edit_file
// ---------------------------------------------------------------------------

func editFileTool() domain.Tool {
	return domain.Tool{
		Name: "edit_file",
		Description: `Make edits to a text file using multi-strategy search and replace.

Replaces 'old_str' with 'new_str'. Tries exact match first, then whitespace-flexible
match, then fuzzy match. Returns a unified diff of the change. If old_str is not found,
returns the most similar chunk as a "did you mean" suggestion.

If the file does not exist and create_if_missing is true, creates it with new_str as content.`,
		InputSchema: schemaFromJSON(`{
			"type": "object",
			"properties": {
				"path": {
					"type": "string",
					"description": "The path to the file."
				},
				"old_str": {
					"type": "string",
					"description": "Text to search for. Must be non-empty for edits."
				},
				"new_str": {
					"type": "string",
					"description": "Text to replace old_str with."
				},
				"count": {
					"type": "integer",
					"description": "Max replacements for exact match (default 1)."
				},
				"create_if_missing": {
					"type": "boolean",
					"description": "Create the file with new_str if it does not exist (default false)."
				}
			},
			"required": ["path", "new_str"]
		}`),
		Source: "code",
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			p := getString(args, "path", "")
			oldStr := getString(args, "old_str", "")
			newStr := getString(args, "new_str", "")
			count := getInt(args, "count", 1)
			createIfMissing := getBool(args, "create_if_missing", false)

			if p == "" {
				return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "path is required"}
			}
			if oldStr == newStr {
				return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "old_str and new_str must be different"}
			}

			data, err := os.ReadFile(p)
			if err != nil {
				if os.IsNotExist(err) {
					if createIfMissing && oldStr == "" {
						return createFile(p, newStr)
					}
					return nil, &domain.StructuredError{Code: "FILE_NOT_FOUND", Message: err.Error()}
				}
				return nil, &domain.StructuredError{Code: "EDIT_FILE_ERROR", Message: err.Error()}
			}

			if oldStr == "" {
				return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "old_str is required when file exists (use write_file to overwrite)"}
			}

			content := string(data)
			result := SearchReplace(content, oldStr, newStr, p, count)

			if result == nil {
				suggestion := FindSimilar(content, oldStr)
				if suggestion != nil {
					return nil, &domain.StructuredError{
						Code: "OLD_STR_NOT_FOUND",
						Message: fmt.Sprintf(
							"The SEARCH text was not found in the file.\n\nDid you mean lines %d-%d (%.0f%% similar)?\n%s\n\nRetry with the exact text shown above.",
							suggestion.StartLine, suggestion.EndLine,
							suggestion.Similarity*100, suggestion.Text,
						),
					}
				}
				return nil, &domain.StructuredError{Code: "OLD_STR_NOT_FOUND", Message: "old_str not found in file"}
			}

			if err := os.WriteFile(p, []byte(result.Content), 0644); err != nil {
				return nil, &domain.StructuredError{Code: "EDIT_FILE_ERROR", Message: err.Error()}
			}

			response := map[string]any{
				"result":   "OK",
				"strategy": result.Strategy,
				"diff":     result.Diff,
			}

			if w := lint(p, result.Content); w != "" {
				response["lint_warnings"] = w
			}

			return response, nil
		},
	}
}

// ---------------------------------------------------------------------------
// write_file
// ---------------------------------------------------------------------------

func writeFileTool() domain.Tool {
	return domain.Tool{
		Name: "write_file",
		Description: `Create a new file or overwrite an existing one.

If the file exists and overwrite is false (default), returns an error.
If the file exists and overwrite is true, overwrites it and returns a diff.
If the file does not exist, creates it with parent directories.`,
		InputSchema: schemaFromJSON(`{
			"type": "object",
			"properties": {
				"path": {
					"type": "string",
					"description": "The path of the file to create or overwrite."
				},
				"content": {
					"type": "string",
					"description": "The content to write."
				},
				"overwrite": {
					"type": "boolean",
					"description": "Allow overwriting existing files (default false)."
				}
			},
			"required": ["path", "content"]
		}`),
		Source: "code",
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			p := getString(args, "path", "")
			content := getString(args, "content", "")
			overwrite := getBool(args, "overwrite", false)

			if p == "" {
				return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "path is required"}
			}

			var diff string
			existing, err := os.ReadFile(p)
			if err == nil {
				if !overwrite {
					return nil, &domain.StructuredError{
						Code:    "FILE_EXISTS",
						Message: fmt.Sprintf("file %s already exists; set overwrite=true to replace it", p),
					}
				}
				diff = generateDiff(string(existing), content, p)
			} else if !os.IsNotExist(err) {
				return nil, &domain.StructuredError{Code: "WRITE_FILE_ERROR", Message: err.Error()}
			}

			dir := filepath.Dir(p)
			if dir != "." && dir != "" {
				if err := os.MkdirAll(dir, 0755); err != nil {
					return nil, &domain.StructuredError{Code: "WRITE_FILE_ERROR", Message: fmt.Sprintf("failed to create directory: %v", err)}
				}
			}

			if err := os.WriteFile(p, []byte(content), 0644); err != nil {
				return nil, &domain.StructuredError{Code: "WRITE_FILE_ERROR", Message: fmt.Sprintf("failed to write file: %v", err)}
			}

			linesWritten := strings.Count(content, "\n")
			if len(content) > 0 && !strings.HasSuffix(content, "\n") {
				linesWritten++
			}

			result := map[string]any{
				"result":        "OK",
				"lines_written": linesWritten,
				"path":          p,
			}
			if diff != "" {
				result["diff"] = diff
			}
			if w := lint(p, content); w != "" {
				result["lint_warnings"] = w
			}
			return result, nil
		},
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func createFile(filePath, content string) (any, error) {
	dir := filepath.Dir(filePath)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, &domain.StructuredError{Code: "CREATE_FILE_ERROR", Message: fmt.Sprintf("failed to create directory: %v", err)}
		}
	}

	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return nil, &domain.StructuredError{Code: "CREATE_FILE_ERROR", Message: fmt.Sprintf("failed to create file: %v", err)}
	}

	linesWritten := strings.Count(content, "\n")
	if len(content) > 0 && !strings.HasSuffix(content, "\n") {
		linesWritten++
	}

	return map[string]any{
		"result":        "OK",
		"lines_written": linesWritten,
		"path":          filePath,
	}, nil
}

func inferLanguage(path string) string {
	ext := filepath.Ext(path)
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js":
		return "javascript"
	case ".ts":
		return "typescript"
	case ".tsx":
		return "tsx"
	case ".jsx":
		return "jsx"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".rb":
		return "ruby"
	case ".sh", ".bash":
		return "bash"
	case ".yaml", ".yml":
		return "yaml"
	case ".json":
		return "json"
	case ".toml":
		return "toml"
	case ".md":
		return "markdown"
	case ".html":
		return "html"
	case ".css":
		return "css"
	case ".sql":
		return "sql"
	case ".c":
		return "c"
	case ".cpp", ".cc", ".cxx":
		return "cpp"
	case ".h", ".hpp":
		return "c/cpp"
	case ".swift":
		return "swift"
	case ".kt":
		return "kotlin"
	case ".xml":
		return "xml"
	case ".proto":
		return "protobuf"
	default:
		return ""
	}
}

// ---------------------------------------------------------------------------
// .gitignore support
// ---------------------------------------------------------------------------

type gitignorePattern struct {
	pattern  string
	negation bool
	dirOnly  bool
}

func parseGitignore(root string) []gitignorePattern {
	f, err := os.Open(filepath.Join(root, ".gitignore"))
	if err != nil {
		return nil
	}
	defer f.Close()

	var patterns []gitignorePattern
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), " \t")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		p := gitignorePattern{}
		if strings.HasPrefix(line, "!") {
			p.negation = true
			line = line[1:]
		}
		if strings.HasSuffix(line, "/") {
			p.dirOnly = true
			line = strings.TrimSuffix(line, "/")
		}
		line = strings.TrimPrefix(line, "/")
		p.pattern = line
		patterns = append(patterns, p)
	}

	return patterns
}

func isIgnored(relPath string, isDir bool, patterns []gitignorePattern) bool {
	if relPath == ".git" || strings.HasPrefix(relPath, ".git"+string(filepath.Separator)) {
		return true
	}

	ignored := false
	name := filepath.Base(relPath)

	for _, p := range patterns {
		if p.dirOnly && !isDir {
			continue
		}

		matched := false

		if m, _ := filepath.Match(p.pattern, name); m {
			matched = true
		}

		if !matched {
			if m, _ := filepath.Match(p.pattern, relPath); m {
				matched = true
			}
		}

		if !matched && strings.Contains(relPath, string(filepath.Separator)) {
			parts := strings.Split(relPath, string(filepath.Separator))
			for _, part := range parts {
				if m, _ := filepath.Match(p.pattern, part); m {
					matched = true
					break
				}
			}
		}

		if matched {
			ignored = !p.negation
		}
	}

	return ignored
}
