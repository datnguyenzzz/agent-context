package callgraph

import (
	"fmt"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
)

type Node struct {
	Name      string `json:"name"`
	FilePath  string `json:"file_path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

type Edge struct {
	Caller string
	Callee string
}

type CallGraph struct {
	Nodes map[string]*Node
	Edges []Edge
}

func ParseFile(path, relPath string) ([]*Node, []Edge, error) {
	fset := token.NewFileSet()
	nodes := make(map[string]*Node)
	var edges []Edge

	ext := strings.ToLower(filepath.Ext(path))
	var err error
	switch ext {
	case ".go":
		err = parseGoFile(path, relPath, fset, nodes, &edges)
	case ".tf":
		err = parseTerraformFile(path, relPath, nodes, &edges)
	case ".yaml", ".yml":
		err = parseYamlFile(path, relPath, nodes, &edges)
	}

	if err != nil {
		return nil, nil, err
	}

	nodeList := make([]*Node, 0, len(nodes))
	for _, n := range nodes {
		nodeList = append(nodeList, n)
	}

	return nodeList, edges, nil
}

func BuildCallGraph(root string) (*CallGraph, error) {
	nodes := make(map[string]*Node)
	var edges []Edge

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "dist" {
				return filepath.SkipDir
			}
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".go" && ext != ".tf" && ext != ".yaml" && ext != ".yml" {
			return nil
		}

		relPath, err := filepath.Rel(root, path)
		if err != nil {
			relPath = path
		}

		fileNodes, fileEdges, err := ParseFile(path, relPath)
		if err == nil {
			for _, n := range fileNodes {
				nodes[n.Name] = n
			}
			edges = append(edges, fileEdges...)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return &CallGraph{
		Nodes: nodes,
		Edges: edges,
	}, nil
}

// GenerateTreeReport creates a beautiful, detailed bi-directional ASCII call tree report
func (cg *CallGraph) GenerateTreeReport(targetFunc string, direction string, maxDepth int) string {
	// Find target node (matching exactly, or matching as a suffix, e.g. resource suffix or method suffix)
	var targetNode *Node
	for name, node := range cg.Nodes {
		if name == targetFunc || strings.HasSuffix(name, "."+targetFunc) {
			targetNode = node
			targetFunc = name // normalize
			break
		}
	}

	if targetNode == nil {
		return fmt.Sprintf("Block/Function '%s' not found. Ensure it is defined in indexed Go, Terraform, or YAML files.", targetFunc)
	}

	report := fmt.Sprintf("🔍 Call/Dependency Graph Exploration for: **%s**\n", targetNode.Name)
	report += fmt.Sprintf("   Declared in: `%s` (Lines: %d-%d)\n\n", targetNode.FilePath, targetNode.StartLine, targetNode.EndLine)

	if direction == "caller" || direction == "both" {
		report += "▲ **UPWARD CHAIN (Who calls/depends on this?):**\n"
		callersTree := cg.buildCallersTree(targetFunc, 0, maxDepth, make(map[string]bool))
		if callersTree == "" {
			report += "   └── (No active upward callers/dependencies found)\n"
		} else {
			report += callersTree
		}
		report += "\n"
	}

	if direction == "callee" || direction == "both" {
		report += "▼ **DOWNWARD CHAIN (What does this call/depend on?):**\n"
		calleesTree := cg.buildCalleesTree(targetFunc, 0, maxDepth, make(map[string]bool))
		if calleesTree == "" {
			report += "   └── (No active downward callees/dependencies found)\n"
		} else {
			report += calleesTree
		}
		report += "\n"
	}

	return report
}

func (cg *CallGraph) buildCallersTree(funcName string, depth, maxDepth int, visited map[string]bool) string {
	if depth >= maxDepth || visited[funcName] {
		return ""
	}
	visited[funcName] = true
	defer func() { visited[funcName] = false }()

	var tree []string
	prefix := strings.Repeat("   ", depth)

	for _, edge := range cg.Edges {
		if edge.Callee == funcName || strings.HasSuffix(edge.Caller, "."+edge.Callee) && edge.Callee == funcName || strings.HasSuffix(funcName, "."+edge.Callee) && edge.Callee != "" {
			callerNode, exists := cg.Nodes[edge.Caller]
			if exists {
				lineStr := fmt.Sprintf("%s└── **%s** (`%s` Lines:%d-%d)\n", prefix, callerNode.Name, callerNode.FilePath, callerNode.StartLine, callerNode.EndLine)
				subTree := cg.buildCallersTree(edge.Caller, depth+1, maxDepth, visited)
				if subTree != "" {
					lineStr += subTree
				}
				tree = append(tree, lineStr)
			}
		}
	}

	return strings.Join(tree, "")
}

func (cg *CallGraph) buildCalleesTree(funcName string, depth, maxDepth int, visited map[string]bool) string {
	if depth >= maxDepth || visited[funcName] {
		return ""
	}
	visited[funcName] = true
	defer func() { visited[funcName] = false }()

	var tree []string
	prefix := strings.Repeat("   ", depth)

	for _, edge := range cg.Edges {
		if edge.Caller == funcName {
			var calleeNode *Node
			for name, node := range cg.Nodes {
				if name == edge.Callee || strings.HasSuffix(name, "."+edge.Callee) {
					calleeNode = node
					break
				}
			}

			if calleeNode != nil {
				lineStr := fmt.Sprintf("%s└── **%s** (`%s` Lines:%d-%d)\n", prefix, calleeNode.Name, calleeNode.FilePath, calleeNode.StartLine, calleeNode.EndLine)
				subTree := cg.buildCalleesTree(calleeNode.Name, depth+1, maxDepth, visited)
				if subTree != "" {
					lineStr += subTree
				}
				tree = append(tree, lineStr)
			}
		}
	}

	return strings.Join(tree, "")
}

// GenerateOnDemandTreeReport creates an ASCII call tree report by fetching callers and callees lazily on the fly via callbacks
func GenerateOnDemandTreeReport(
	targetNode *Node,
	direction string,
	maxDepth int,
	getCallees func(caller string) ([]*Node, error),
	getCallers func(callee string) ([]*Node, error),
) string {
	report := fmt.Sprintf("🔍 Call/Dependency Graph Exploration for: **%s**\n", targetNode.Name)
	report += fmt.Sprintf("   Declared in: `%s` (Lines: %d-%d)\n\n", targetNode.FilePath, targetNode.StartLine, targetNode.EndLine)

	if direction == "caller" || direction == "both" {
		report += "▲ **UPWARD CHAIN (Who calls/depends on this?):**\n"
		callersTree := buildOnDemandCallersTree(targetNode.Name, 0, maxDepth, make(map[string]bool), getCallers)
		if callersTree == "" {
			report += "   └── (No active upward callers/dependencies found)\n"
		} else {
			report += callersTree
		}
		report += "\n"
	}

	if direction == "callee" || direction == "both" {
		report += "▼ **DOWNWARD CHAIN (What does this call/depend on?):**\n"
		calleesTree := buildOnDemandCalleesTree(targetNode.Name, 0, maxDepth, make(map[string]bool), getCallees)
		if calleesTree == "" {
			report += "   └── (No active downward callees/dependencies found)\n"
		} else {
			report += calleesTree
		}
		report += "\n"
	}

	return report
}

func buildOnDemandCallersTree(
	funcName string,
	depth, maxDepth int,
	visited map[string]bool,
	getCallers func(callee string) ([]*Node, error),
) string {
	if depth >= maxDepth || visited[funcName] {
		return ""
	}
	visited[funcName] = true
	defer func() { visited[funcName] = false }()

	callers, err := getCallers(funcName)
	if err != nil || len(callers) == 0 {
		return ""
	}

	var tree []string
	prefix := strings.Repeat("   ", depth)

	for _, callerNode := range callers {
		lineStr := fmt.Sprintf("%s└── **%s** (`%s` Lines:%d-%d)\n", prefix, callerNode.Name, callerNode.FilePath, callerNode.StartLine, callerNode.EndLine)
		subTree := buildOnDemandCallersTree(callerNode.Name, depth+1, maxDepth, visited, getCallers)
		if subTree != "" {
			lineStr += subTree
		}
		tree = append(tree, lineStr)
	}

	return strings.Join(tree, "")
}

func buildOnDemandCalleesTree(
	funcName string,
	depth, maxDepth int,
	visited map[string]bool,
	getCallees func(caller string) ([]*Node, error),
) string {
	if depth >= maxDepth || visited[funcName] {
		return ""
	}
	visited[funcName] = true
	defer func() { visited[funcName] = false }()

	callees, err := getCallees(funcName)
	if err != nil || len(callees) == 0 {
		return ""
	}

	var tree []string
	prefix := strings.Repeat("   ", depth)

	for _, calleeNode := range callees {
		lineStr := fmt.Sprintf("%s└── **%s** (`%s` Lines:%d-%d)\n", prefix, calleeNode.Name, calleeNode.FilePath, calleeNode.StartLine, calleeNode.EndLine)
		subTree := buildOnDemandCalleesTree(calleeNode.Name, depth+1, maxDepth, visited, getCallees)
		if subTree != "" {
			lineStr += subTree
		}
		tree = append(tree, lineStr)
	}

	return strings.Join(tree, "")
}
