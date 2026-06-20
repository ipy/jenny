package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TreeTool lists directory contents recursively as a tree.
type TreeTool struct {
	permissionLevel PermissionLevel
	sessionID       string
}

func NewTreeTool(level PermissionLevel) *TreeTool {
	return &TreeTool{permissionLevel: level}
}

func (t *TreeTool) Name() string { return "tree" }
func (t *TreeTool) effectiveLevel() PermissionLevel {
	return t.permissionLevel
}
func (t *TreeTool) WithSessionID(id string) *TreeTool {
	t.sessionID = id
	return t
}

func (t *TreeTool) Description() string {
	return "Recursively list directory contents as a tree with type suffixes and entry counts."
}

func (t *TreeTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "The directory path to list",
			},
			"max_depth": map[string]any{
				"type":        "number",
				"description": "Max recursion depth. Default 2.",
			},
			"show_hidden": map[string]any{
				"type":        "boolean",
				"description": "Show hidden files (dotfiles). Default false.",
			},
			"cursor": map[string]any{
				"type":        "string",
				"description": "Pagination cursor from previous call",
			},
			"limit": map[string]any{
				"type":        "number",
				"description": "Max entries per page. Default 100.",
			},
		},
	}
}

func (t *TreeTool) Execute(ctx context.Context, input map[string]any, cwd string) (*ToolResult, error) {
	path := ""
	if p, ok := input["path"].(string); ok && p != "" {
		path = p
	}
	maxDepth := 2
	if d, ok := input["max_depth"].(float64); ok && int(d) > 0 {
		maxDepth = int(d)
	}
	showHidden := false
	if h, ok := input["show_hidden"].(bool); ok {
		showHidden = h
	}
	limit := 100
	if l, ok := input["limit"].(float64); ok && int(l) > 0 {
		limit = int(l)
	}
	cursor := ""
	if c, ok := input["cursor"].(string); ok {
		cursor = c
	}

	if path == "" {
		path = cwd
	} else if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}

	absPath, err := NormalizePath(path)
	if err != nil {
		return &ToolResult{Content: "Error: " + err.Error(), IsError: true}, nil
	}

	absCwd, _ := filepath.Abs(cwd)
	if t.effectiveLevel().PathConstrained() {
		sep := string(filepath.Separator)
		if !strings.HasPrefix(absPath+sep, absCwd+sep) && absPath != absCwd {
			return &ToolResult{
				Content: fmt.Sprintf("Error: Access to '%s' is not allowed.", path),
				IsError: true,
			}, nil
		}
	}

	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &ToolResult{Content: "Error: directory does not exist: " + path, IsError: true}, nil
		}
		return &ToolResult{Content: "Error: " + err.Error(), IsError: true}, nil
	}
	if !info.IsDir() {
		return &ToolResult{Content: "Error: '" + path + "' is not a directory", IsError: true}, nil
	}

	tree, nextCursor, truncated := t.buildTree(absPath, maxDepth, showHidden, limit, cursor)

	var content strings.Builder
	fmt.Fprintf(&content, "/%s\n", filepath.Base(absPath))
	content.WriteString(tree)
	if nextCursor != "" {
		fmt.Fprintf(&content, "\n[... continue with cursor: %s]", nextCursor)
	} else if truncated {
		fmt.Fprintf(&content, "\n[... truncated]")
	}

	return &ToolResult{
		Content:   content.String(),
		IsError:   false,
		Truncated: truncated,
	}, nil
}

type treeItem struct {
	path     string
	name     string
	isDir    bool
	depth    int
	isLast   bool
	upPrefix string // prefix for │ vs spaces above this node
}

// buildTree returns the tree body, next cursor, and whether truncated.
func (t *TreeTool) buildTree(root string, maxDepth int, showHidden bool, limit int, cursor string) (string, string, bool) {
	var body strings.Builder
	entriesListed := 0
	var nextCursor string
	truncated := false

	// BFS queue: each entry is a file/dir to format and (if dir) expand
	type queueItem struct {
		item  treeItem
		child bool // true if this is a child of a directory (has upPrefix)
	}
	queue := make([]queueItem, 0, 32)

	// Seed: list root directory and populate queue with its children
	rootEntries, err := os.ReadDir(root)
	if err != nil {
		return "", "", false
	}

	// Filter root entries, apply cursor
	var filtered []os.DirEntry
	seenCursor := cursor == ""
	for _, de := range rootEntries {
		if !showHidden && strings.HasPrefix(de.Name(), ".") {
			continue
		}
		if !seenCursor {
			if de.Name() == cursor {
				seenCursor = true
			}
			continue
		}
		filtered = append(filtered, de)
	}

	if !seenCursor && cursor != "" {
		return "", "", false
	}

	// Apply limit
	if len(filtered) > limit {
		nextCursor = filtered[limit].Name()
		filtered = filtered[:limit]
		truncated = true
	}

	// Build initial queue from root's immediate children
	for i, de := range filtered {
		isLast := i == len(filtered)-1
		item := treeItem{
			path:   filepath.Join(root, de.Name()),
			name:   de.Name(),
			isDir:  de.IsDir(),
			depth:  1,
			isLast: isLast,
		}
		queue = append(queue, queueItem{item: item})
	}

	// Process queue
	for len(queue) > 0 {
		qi := queue[0]
		queue = queue[1:]
		it := qi.item

		if entriesListed >= limit && !truncated {
			truncated = true
			break
		}
		entriesListed++

		// Determine line prefix
		var linePrefix string
		if it.isLast {
			linePrefix = "└── "
		} else {
			linePrefix = "├── "
		}

		// Build hint
		hint := ""
		if it.isDir {
			count, _ := CountDirEntries(it.path)
			switch count {
			case 0:
				hint = "[empty]"
			case 1:
				hint = "[1 entry]"
			default:
				hint = fmt.Sprintf("[%d entries]", count)
			}
			it.name += "/"
		} else {
			if info, err := os.Stat(it.path); err == nil {
				hint = fmt.Sprintf("[%d bytes, %s]", info.Size(), info.ModTime().Format("2006-01-02"))
			}
		}

		fmt.Fprintf(&body, "%s%s\t%s\n", linePrefix, it.name, hint)

		// Recurse into directories within depth limit
		if it.isDir && it.depth < maxDepth {
			subs, err := os.ReadDir(it.path)
			if err != nil {
				continue
			}
			// Filter hidden
			var children []os.DirEntry
			for _, se := range subs {
				if !showHidden && strings.HasPrefix(se.Name(), ".") {
					continue
				}
				children = append(children, se)
			}
			// Add to front of queue (so children appear before siblings at same level)
			prefix := "│   "
			if it.isLast {
				prefix = "    "
			}
			for i := len(children) - 1; i >= 0; i-- {
				isLast := i == 0
				child := treeItem{
					path:     filepath.Join(it.path, children[i].Name()),
					name:     children[i].Name(),
					isDir:    children[i].IsDir(),
					depth:    it.depth + 1,
					isLast:   isLast,
					upPrefix: prefix,
				}
				queue = append([]queueItem{{item: child, child: true}}, queue...)
			}
		}
	}

	return body.String(), nextCursor, truncated
}