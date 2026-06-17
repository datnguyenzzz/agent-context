package main

// ponytail: session-start hook loads local database memories by cwd, ultra-fast, zero external API queries on startup

import (
	"encoding/json"
	"fmt"
	"os"

	"agent-mem/internal/db"
)

type StartupPayload struct {
	CWD string `json:"cwd"`
}

type StartupResponse struct {
	SystemMessage      string                 `json:"systemMessage,omitempty"`
	HookSpecificOutput *StartupSpecificOutput `json:"hookSpecificOutput,omitempty"`
}

type StartupSpecificOutput struct {
	AdditionalContext string `json:"additionalContext"`
}

func main() {
	var payload StartupPayload
	if err := json.NewDecoder(os.Stdin).Decode(&payload); err != nil {
		// Fallback gracefully on stdin parse failure
		fmt.Println("{}")
		return
	}

	cwd := payload.CWD
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	// Ensure table is initialized
	if err := db.InitDatabase(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to init database: %v\n", err)
		fmt.Println("{}")
		return
	}

	// Fetch up to 10 most recent memories for this workspace or general personal memories
	memories, err := db.GetRecentMemories(cwd, 10)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to retrieve memories: %v\n", err)
		fmt.Println("{}")
		return
	}

	if len(memories) == 0 {
		fmt.Println("{}")
		return
	}

	var formatted string
	for i, row := range memories {
		formatted += fmt.Sprintf("[%d] (%s) Saved on %s:\n%s\n\n", i+1, stringsToUpper(row.Category), row.CreatedAt.Format("2006-01-02 15:04:05"), row.Content)
	}

	additionalContext := fmt.Sprintf("### RETRIEVED PERSISTENT MEMORIES\nBelow are relevant personal preferences, decisions, and project memories retrieved across past sessions. Adhere to these guidelines, preferences, and facts:\n\n%s", formatted)

	resp := StartupResponse{
		SystemMessage: fmt.Sprintf("Loaded %d memories for persistent session context.", len(memories)),
		HookSpecificOutput: &StartupSpecificOutput{
			AdditionalContext: additionalContext,
		},
	}

	outBytes, _ := json.Marshal(resp)
	fmt.Println(string(outBytes))
}

func stringsToUpper(s string) string {
	switch s {
	case "personal":
		return "PERSONAL"
	case "project":
		return "PROJECT"
	default:
		return "UNKNOWN"
	}
}
