package splitter

// ponytail: parse Markdown files semantically by splitting along heading boundaries (# , ## , ### ), keeping sections intact for maximum context cohesion

import (
	"os"
	"strings"
)

func parseMarkdownFile(filePath string) ([]Chunk, error) {
	contentBytes, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(contentBytes), "\n")
	var chunks []Chunk

	var currentBlock []string
	blockStartLine := 0
	inBlock := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Check if the line is a heading (starts with "# ", "## ", "### ", "#### ", "##### ", "###### ")
		isHeading := false
		if strings.HasPrefix(trimmed, "# ") ||
			strings.HasPrefix(trimmed, "## ") ||
			strings.HasPrefix(trimmed, "### ") ||
			strings.HasPrefix(trimmed, "#### ") ||
			strings.HasPrefix(trimmed, "##### ") ||
			strings.HasPrefix(trimmed, "###### ") {
			isHeading = true
		}

		if isHeading {
			// Save current block before starting new one
			if inBlock && len(currentBlock) > 0 {
				chunks = append(chunks, Chunk{
					FilePath:  filePath,
					Content:   strings.Join(currentBlock, "\n"),
					StartLine: blockStartLine,
					EndLine:   i,
				})
			}
			inBlock = true
			blockStartLine = i + 1
			currentBlock = []string{line}
		} else {
			if inBlock {
				currentBlock = append(currentBlock, line)
			} else {
				// Start block for initial content (e.g., frontmatter, text before first header)
				inBlock = true
				blockStartLine = i + 1
				currentBlock = []string{line}
			}
		}
	}

	// Add the last block
	if inBlock && len(currentBlock) > 0 {
		chunks = append(chunks, Chunk{
			FilePath:  filePath,
			Content:   strings.Join(currentBlock, "\n"),
			StartLine: blockStartLine,
			EndLine:   len(lines),
		})
	}

	// Fallback if no chunks found
	if len(chunks) == 0 {
		chunks = append(chunks, Chunk{
			FilePath:  filePath,
			Content:   string(contentBytes),
			StartLine: 1,
			EndLine:   len(lines),
		})
	}

	return chunks, nil
}
