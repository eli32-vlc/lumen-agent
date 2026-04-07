package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func (r *Registry) lockPaths(ctx context.Context, paths ...string) (func(), error) {
	keys := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		keys = append(keys, "file:"+filepath.Clean(path))
	}
	return r.ensureLockManager().Acquire(ctx, keys...)
}

func (r *Registry) registerFilesystemTools() {
	r.register(
		"read_file",
		"Read a file inside the workspace. Optionally return only a specific line range and/or cap the returned content size.",
		objectSchema(map[string]any{
			"path":       stringSchema("Path to the file, relative to the workspace root when not absolute."),
			"start_line": integerSchema("Optional 1-based start line.", 1),
			"end_line":   integerSchema("Optional 1-based end line.", 1),
			"max_bytes":  integerSchema("Optional soft cap for returned content bytes. The runtime applies a hard upper bound as well.", 1),
		}, "path"),
		r.handleReadFile,
	)

	r.register(
		"write_file",
		"Write a file inside the workspace. Parent directories are created automatically.",
		objectSchema(map[string]any{
			"path":      stringSchema("Destination path."),
			"content":   stringSchema("Full file content to write."),
			"overwrite": booleanSchema("When false, fail if the file already exists."),
		}, "path", "content"),
		r.handleWriteFile,
	)

	r.register(
		"replace_in_file",
		"Replace exact text in an existing file.",
		objectSchema(map[string]any{
			"path":        stringSchema("Path to the file to edit."),
			"old_text":    stringSchema("Existing text to replace."),
			"new_text":    stringSchema("Replacement text."),
			"replace_all": booleanSchema("Replace all matches instead of exactly one."),
		}, "path", "old_text", "new_text"),
		r.handleReplaceInFile,
	)

	r.register(
		"list_dir",
		"List files and directories under a path inside the workspace.",
		objectSchema(map[string]any{
			"path":           stringSchema("Directory to inspect. Defaults to the workspace root."),
			"recursive":      booleanSchema("Whether to recurse into subdirectories."),
			"include_hidden": booleanSchema("Whether to include dotfiles and dot-directories."),
		}),
		r.handleListDir,
	)

	r.register(
		"grep_search",
		"Search file contents using a regular expression.",
		objectSchema(map[string]any{
			"pattern":        stringSchema("Regular expression to search for."),
			"path":           stringSchema("File or directory to search from. Defaults to the workspace root."),
			"max_results":    integerSchema("Maximum number of matches to return.", 1),
			"case_sensitive": booleanSchema("Whether the regular expression should be case-sensitive."),
		}, "pattern"),
		r.handleGrepSearch,
	)

	r.register(
		"mkdir",
		"Create a directory and any missing parents.",
		objectSchema(map[string]any{
			"path": stringSchema("Directory path to create."),
		}, "path"),
		r.handleMkdir,
	)

	r.register(
		"move_path",
		"Move or rename a file or directory inside the workspace.",
		objectSchema(map[string]any{
			"source":      stringSchema("Current file or directory path."),
			"destination": stringSchema("Target path."),
			"overwrite":   booleanSchema("Whether to replace the destination when it already exists."),
		}, "source", "destination"),
		r.handleMovePath,
	)

	r.register(
		"delete_path",
		"Delete a file or directory inside the workspace.",
		objectSchema(map[string]any{
			"path":      stringSchema("File or directory path to delete."),
			"recursive": booleanSchema("Required for directory removal."),
		}, "path"),
		r.handleDeletePath,
	)
}

func (r *Registry) handleReadFile(_ context.Context, payload json.RawMessage) (string, error) {
	type args struct {
		Path      string `json:"path"`
		StartLine int    `json:"start_line"`
		EndLine   int    `json:"end_line"`
		MaxBytes  int    `json:"max_bytes"`
	}

	var input args
	if err := decodeArgs(payload, &input); err != nil {
		return "", err
	}

	path, err := r.resolvePath(input.Path)
	if err != nil {
		return "", err
	}
	if err := r.ensurePathAccessible(path); err != nil {
		return "", err
	}

	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat file: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory", r.relPath(path))
	}
	start := input.StartLine
	if start <= 0 {
		start = 1
	}

	requestedEnd := input.EndLine
	if requestedEnd > 0 && start > requestedEnd {
		return "", fmt.Errorf("start_line must be less than or equal to end_line")
	}

	hardMaxBytes := r.readFileHardMaxBytes()
	appliedMaxBytes := hardMaxBytes
	if input.MaxBytes > 0 && input.MaxBytes < appliedMaxBytes {
		appliedMaxBytes = input.MaxBytes
	}

	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	var builder strings.Builder
	totalLines := 0
	lastReturnedLine := 0
	truncatedByLimit := false
	partialLastLine := false

	for {
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", fmt.Errorf("read file: %w", err)
		}
		if err == io.EOF && line == "" {
			break
		}

		totalLines++
		withinRange := totalLines >= start && (requestedEnd <= 0 || totalLines <= requestedEnd)
		if withinRange {
			remaining := appliedMaxBytes - builder.Len()
			if remaining <= 0 {
				truncatedByLimit = true
			} else {
				chunk := line
				if len(chunk) > remaining {
					chunk = prefixByBytes(chunk, remaining)
					truncatedByLimit = true
					partialLastLine = true
				}
				if chunk != "" {
					builder.WriteString(chunk)
					lastReturnedLine = totalLines
				}
			}
		}

		if err == io.EOF {
			break
		}
	}

	end := requestedEnd
	if end <= 0 || end > totalLines {
		end = totalLines
	}

	content := builder.String()
	truncated := start != 1 || end != totalLines || truncatedByLimit
	nextStartLine := 0
	if partialLastLine && lastReturnedLine > 0 {
		nextStartLine = lastReturnedLine
	} else if lastReturnedLine > 0 && lastReturnedLine < totalLines {
		nextStartLine = lastReturnedLine + 1
	}

	return jsonResult(map[string]any{
		"path":                r.relPath(path),
		"start_line":          start,
		"end_line":            end,
		"returned_end_line":   lastReturnedLine,
		"next_start_line":     nextStartLine,
		"total_lines":         totalLines,
		"file_size_bytes":     info.Size(),
		"requested_max_bytes": input.MaxBytes,
		"applied_max_bytes":   appliedMaxBytes,
		"bytes_returned":      len(content),
		"truncated":           truncated,
		"truncated_by_max":    truncatedByLimit,
		"partial_last_line":   partialLastLine,
		"content":             content,
	})
}

func (r *Registry) readFileHardMaxBytes() int {
	hardMax := int(r.cfg.Tools.MaxFileBytes)
	if hardMax <= 0 {
		hardMax = r.cfg.Tools.MaxCommandOutputBytes
	}
	if r.cfg.Tools.MaxCommandOutputBytes > 0 && (hardMax <= 0 || r.cfg.Tools.MaxCommandOutputBytes < hardMax) {
		hardMax = r.cfg.Tools.MaxCommandOutputBytes
	}
	if hardMax <= 0 {
		return 64 * 1024
	}
	return hardMax
}

func prefixByBytes(value string, limit int) string {
	if limit <= 0 || value == "" {
		return ""
	}
	if len(value) <= limit {
		return value
	}

	cut := 0
	for index := range value {
		if index > limit {
			break
		}
		cut = index
	}
	if cut == 0 {
		return ""
	}
	return value[:cut]
}

func (r *Registry) handleWriteFile(ctx context.Context, payload json.RawMessage) (string, error) {
	type args struct {
		Path      string `json:"path"`
		Content   string `json:"content"`
		Overwrite bool   `json:"overwrite"`
	}

	var input args
	if err := decodeArgs(payload, &input); err != nil {
		return "", err
	}

	path, err := r.resolvePath(input.Path)
	if err != nil {
		return "", err
	}
	if err := r.ensurePathAccessible(path); err != nil {
		return "", err
	}
	release, err := r.lockPaths(ctx, path)
	if err != nil {
		return "", err
	}
	defer release()

	if !input.Overwrite {
		if _, err := os.Stat(path); err == nil {
			return "", fmt.Errorf("%s already exists and overwrite is false", r.relPath(path))
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("create parent directories: %w", err)
	}

	if err := os.WriteFile(path, []byte(input.Content), 0o644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}

	preview, truncated, lineCount := previewTextContent(input.Content, 12, 600)

	return jsonResult(map[string]any{
		"path":              r.relPath(path),
		"bytes_written":     len(input.Content),
		"line_count":        lineCount,
		"content_preview":   preview,
		"truncated_preview": truncated,
	})
}

func (r *Registry) handleReplaceInFile(ctx context.Context, payload json.RawMessage) (string, error) {
	type args struct {
		Path       string `json:"path"`
		OldText    string `json:"old_text"`
		NewText    string `json:"new_text"`
		ReplaceAll bool   `json:"replace_all"`
	}

	var input args
	if err := decodeArgs(payload, &input); err != nil {
		return "", err
	}

	if input.OldText == "" {
		return "", fmt.Errorf("old_text must not be empty")
	}

	path, err := r.resolvePath(input.Path)
	if err != nil {
		return "", err
	}
	if err := r.ensurePathAccessible(path); err != nil {
		return "", err
	}
	release, err := r.lockPaths(ctx, path)
	if err != nil {
		return "", err
	}
	defer release()

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}

	content := string(data)
	matches := strings.Count(content, input.OldText)
	if matches == 0 {
		return "", fmt.Errorf("old_text was not found in %s", r.relPath(path))
	}
	if matches > 1 && !input.ReplaceAll {
		return "", fmt.Errorf("old_text matched %d times; set replace_all to true to continue", matches)
	}

	count := 1
	if input.ReplaceAll {
		count = -1
	}
	updated := strings.Replace(content, input.OldText, input.NewText, count)

	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}

	replaced := 1
	if input.ReplaceAll {
		replaced = matches
	}

	preview, truncated, lineCount := previewTextContent(updated, 12, 600)

	return jsonResult(map[string]any{
		"path":              r.relPath(path),
		"matches_replaced":  replaced,
		"line_count":        lineCount,
		"content_preview":   preview,
		"truncated_preview": truncated,
	})
}

func (r *Registry) handleListDir(_ context.Context, payload json.RawMessage) (string, error) {
	type args struct {
		Path          string `json:"path"`
		Recursive     bool   `json:"recursive"`
		IncludeHidden bool   `json:"include_hidden"`
	}

	var input args
	if err := decodeArgs(payload, &input); err != nil {
		return "", err
	}

	path, err := r.resolvePath(input.Path)
	if err != nil {
		return "", err
	}

	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat path: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", r.relPath(path))
	}

	type entry struct {
		Path string `json:"path"`
		Type string `json:"type"`
		Size int64  `json:"size,omitempty"`
	}

	entries := make([]entry, 0, 32)
	limit := r.cfg.Tools.MaxSearchResults * 10
	truncated := false
	dirCount := 0
	fileCount := 0

	if input.Recursive {
		walkErr := filepath.WalkDir(path, func(current string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if current == path {
				return nil
			}

			name := d.Name()
			if !input.IncludeHidden && strings.HasPrefix(name, ".") {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}

			if len(entries) >= limit {
				truncated = true
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}

			item := entry{Path: r.relPath(current)}
			if d.IsDir() {
				item.Type = "dir"
				dirCount++
			} else {
				item.Type = "file"
				fileCount++
				if info, err := d.Info(); err == nil {
					item.Size = info.Size()
				}
			}
			entries = append(entries, item)
			return nil
		})
		if walkErr != nil {
			return "", fmt.Errorf("walk directory: %w", walkErr)
		}
	} else {
		dirEntries, err := os.ReadDir(path)
		if err != nil {
			return "", fmt.Errorf("read directory: %w", err)
		}

		for _, dirEntry := range dirEntries {
			name := dirEntry.Name()
			if !input.IncludeHidden && strings.HasPrefix(name, ".") {
				continue
			}
			item := entry{Path: filepath.ToSlash(filepath.Join(r.relPath(path), name))}
			if r.relPath(path) == "." {
				item.Path = filepath.ToSlash(name)
			}
			if dirEntry.IsDir() {
				item.Type = "dir"
				dirCount++
			} else {
				item.Type = "file"
				fileCount++
				if info, err := dirEntry.Info(); err == nil {
					item.Size = info.Size()
				}
			}
			entries = append(entries, item)
		}
	}

	return jsonResult(map[string]any{
		"path":        r.relPath(path),
		"entries":     entries,
		"entry_count": len(entries),
		"dir_count":   dirCount,
		"file_count":  fileCount,
		"truncated":   truncated,
	})
}

func (r *Registry) handleGrepSearch(_ context.Context, payload json.RawMessage) (string, error) {
	type args struct {
		Pattern       string `json:"pattern"`
		Path          string `json:"path"`
		MaxResults    int    `json:"max_results"`
		CaseSensitive bool   `json:"case_sensitive"`
	}

	var input args
	if err := decodeArgs(payload, &input); err != nil {
		return "", err
	}

	pattern := input.Pattern
	if !input.CaseSensitive {
		pattern = "(?i)" + pattern
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("compile regex: %w", err)
	}

	rootPath, err := r.resolvePath(input.Path)
	if err != nil {
		return "", err
	}

	maxResults := input.MaxResults
	if maxResults <= 0 {
		maxResults = r.cfg.Tools.MaxSearchResults
	}

	type match struct {
		Path       string `json:"path"`
		LineNumber int    `json:"line_number"`
		Line       string `json:"line"`
	}

	matches := make([]match, 0, maxResults)
	truncated := false

	searchFile := func(path string) error {
		if r.isProtectedPath(path) {
			return nil
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if info.IsDir() || info.Size() > r.cfg.Tools.MaxFileBytes {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		buffer := make([]byte, 0, 64*1024)
		scanner.Buffer(buffer, int(r.cfg.Tools.MaxFileBytes))

		lineNumber := 0
		for scanner.Scan() {
			lineNumber++
			line := scanner.Text()
			if re.MatchString(line) {
				matches = append(matches, match{
					Path:       r.relPath(path),
					LineNumber: lineNumber,
					Line:       line,
				})
				if len(matches) >= maxResults {
					truncated = true
					return nil
				}
			}
		}

		return scanner.Err()
	}

	info, err := os.Stat(rootPath)
	if err != nil {
		return "", fmt.Errorf("stat search path: %w", err)
	}
	pathType := "file"
	if !info.IsDir() {
		if err := r.ensurePathAccessible(rootPath); err != nil {
			return "", err
		}
	}

	if info.IsDir() {
		pathType = "dir"
		walkErr := filepath.WalkDir(rootPath, func(current string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				if strings.HasPrefix(d.Name(), ".") && current != rootPath {
					return filepath.SkipDir
				}
				return nil
			}
			if truncated {
				return nil
			}
			return searchFile(current)
		})
		if walkErr != nil {
			return "", fmt.Errorf("walk search path: %w", walkErr)
		}
	} else {
		if err := searchFile(rootPath); err != nil {
			return "", fmt.Errorf("search file: %w", err)
		}
	}

	return jsonResult(map[string]any{
		"pattern":            input.Pattern,
		"path":               r.relPath(rootPath),
		"searched_path_type": pathType,
		"match_count":        len(matches),
		"matches":            matches,
		"truncated":          truncated,
	})
}

func (r *Registry) handleMkdir(ctx context.Context, payload json.RawMessage) (string, error) {
	type args struct {
		Path string `json:"path"`
	}

	var input args
	if err := decodeArgs(payload, &input); err != nil {
		return "", err
	}

	path, err := r.resolvePath(input.Path)
	if err != nil {
		return "", err
	}
	if err := r.ensurePathAccessible(path); err != nil {
		return "", err
	}
	release, err := r.lockPaths(ctx, path)
	if err != nil {
		return "", err
	}
	defer release()

	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", fmt.Errorf("create directory: %w", err)
	}

	parent := filepath.Dir(path)
	entries, truncated := directoryEntriesPreview(parent, 12)

	return jsonResult(map[string]any{
		"path":                     r.relPath(path),
		"parent_path":              r.relPath(parent),
		"parent_entries_preview":   entries,
		"truncated_parent_preview": truncated,
	})
}

func (r *Registry) handleMovePath(ctx context.Context, payload json.RawMessage) (string, error) {
	type args struct {
		Source      string `json:"source"`
		Destination string `json:"destination"`
		Overwrite   bool   `json:"overwrite"`
	}

	var input args
	if err := decodeArgs(payload, &input); err != nil {
		return "", err
	}

	source, err := r.resolvePath(input.Source)
	if err != nil {
		return "", err
	}
	if err := r.ensurePathMutationAllowed(source); err != nil {
		return "", err
	}
	destination, err := r.resolvePath(input.Destination)
	if err != nil {
		return "", err
	}
	if err := r.ensurePathMutationAllowed(destination); err != nil {
		return "", err
	}
	release, err := r.lockPaths(ctx, source, destination)
	if err != nil {
		return "", err
	}
	defer release()

	if source == r.root {
		return "", fmt.Errorf("refusing to move the workspace root")
	}

	if !input.Overwrite {
		if _, err := os.Stat(destination); err == nil {
			return "", fmt.Errorf("%s already exists and overwrite is false", r.relPath(destination))
		}
	} else if _, err := os.Stat(destination); err == nil {
		if err := os.RemoveAll(destination); err != nil {
			return "", fmt.Errorf("remove existing destination: %w", err)
		}
	}

	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return "", fmt.Errorf("create destination parent directories: %w", err)
	}

	if err := os.Rename(source, destination); err != nil {
		return "", fmt.Errorf("move path: %w", err)
	}

	parent := filepath.Dir(destination)
	entries, truncated := directoryEntriesPreview(parent, 12)

	return jsonResult(map[string]any{
		"source":                   r.relPath(source),
		"destination":              r.relPath(destination),
		"destination_parent_path":  r.relPath(parent),
		"parent_entries_preview":   entries,
		"truncated_parent_preview": truncated,
	})
}

func (r *Registry) handleDeletePath(ctx context.Context, payload json.RawMessage) (string, error) {
	type args struct {
		Path      string `json:"path"`
		Recursive bool   `json:"recursive"`
	}

	var input args
	if err := decodeArgs(payload, &input); err != nil {
		return "", err
	}

	path, err := r.resolvePath(input.Path)
	if err != nil {
		return "", err
	}
	if err := r.ensurePathMutationAllowed(path); err != nil {
		return "", err
	}
	release, err := r.lockPaths(ctx, path)
	if err != nil {
		return "", err
	}
	defer release()
	if path == r.root {
		return "", fmt.Errorf("refusing to delete the workspace root")
	}

	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat path: %w", err)
	}

	if info.IsDir() {
		if !input.Recursive {
			return "", fmt.Errorf("%s is a directory; set recursive to true to delete it", r.relPath(path))
		}
		if err := os.RemoveAll(path); err != nil {
			return "", fmt.Errorf("delete directory: %w", err)
		}
	} else {
		if err := os.Remove(path); err != nil {
			return "", fmt.Errorf("delete file: %w", err)
		}
	}

	parent := filepath.Dir(path)
	entries, truncated := directoryEntriesPreview(parent, 12)

	return jsonResult(map[string]any{
		"path":                     r.relPath(path),
		"deleted_type":             deletedPathType(info),
		"parent_path":              r.relPath(parent),
		"parent_entries_preview":   entries,
		"truncated_parent_preview": truncated,
	})
}

func previewTextContent(content string, maxLines int, maxChars int) (string, bool, int) {
	if maxLines <= 0 {
		maxLines = 12
	}
	if maxChars <= 0 {
		maxChars = 600
	}

	lineCount := 0
	if content != "" {
		lineCount = strings.Count(content, "\n") + 1
	}

	lines := strings.Split(content, "\n")
	truncated := false
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		truncated = true
	}

	preview := strings.Join(lines, "\n")
	if len(preview) > maxChars {
		preview = preview[:maxChars]
		truncated = true
	}

	return preview, truncated, lineCount
}

func directoryEntriesPreview(path string, limit int) ([]string, bool) {
	if limit <= 0 {
		limit = 12
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, false
	}

	preview := make([]string, 0, min(limit, len(entries)))
	truncated := false
	for _, entry := range entries {
		if len(preview) >= limit {
			truncated = true
			break
		}

		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		preview = append(preview, name)
	}

	return preview, truncated
}

func deletedPathType(info os.FileInfo) string {
	if info == nil {
		return ""
	}
	if info.IsDir() {
		return "dir"
	}
	return "file"
}
