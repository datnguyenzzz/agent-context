# Gemini CLI Persistent Memory & Merkle Tree Indexer Extension

A high-performance, model-agnostic, and compiler-grade Gemini CLI extension that equips your AI agent with persistent, long-term memory across sessions and ultra-fast codebase indexing. 

Written in **Go**, powered by **DuckDB** for instantaneous embedded vector similarity searches, and routed via **LiteLLM** (supporting any OpenAI-compatible API, local or cloud).

---

## 🚀 Core Capabilities

### 1. Merkle Tree-Based Incremental Indexing
Instead of performing slow and expensive full-reindexing on every codebase scan, the extension utilizes a hierarchical local **Merkle Tree**:
* **Instant Delta Detection:** Builds file-level and directory-level cryptographic SHA-256 hashes to represent the codebase state. On subsequent runs, it diffs the new tree against the old state to isolate added, modified, and deleted files in milliseconds.
* **Redundant-Free Vectorization:** Avoids chunking or calling the LLM embedding API for unchanged files.
* **Automatic Garbage Collection:** Automatically deletes stale chunks and vectors belonging to deleted or modified files in DuckDB, keeping your index 100% clean and up-to-date.

### 2. Multi-Codebase Architecture Profile Portfolio
Optimized to handle multiple repositories seamlessly, preventing your agent from getting confused between similar files in different projects:
* **Automatic Codebase Profiling:** After indexing, the extension summarizes the directory structure and file composition into an architectural profile detailing the project's tech stack, directory structure, and main purpose. This is registered as a global profile memory.
* **Explicit Origin Tagging:** Every result returned via `search_memory` is clearly labeled with its source repository and absolute directory path:
  `[Codebase: agent-mem] [Path: /Users/thanh.nguyen/agent-mem] [Category: project] (Similarity: 88.5%)`
* **Active Codebase Discovery:** Exposes a `list_codebases` discovery tool allowing the LLM to actively browse the portfolios of all indexed codebases on your machine.

### 3. Compiler-Grade AST & Semantic Code Splitters
Includes dedicated, highly semantic, and zero-dependency parsers for code and documentation files:
* **Go (AST-Based):** Slices package-level declarations (`FuncDecl`, `TypeSpec` structs/interfaces, `ValueSpec` variables/constants) separately. It is **doc-comment aware**, automatically shifting chunk boundaries to include leading block `/* */` and line `//` comments so the LLM gets full doc context.
* **Terraform (Lexical Block-Based):** Groups configuration blocks (`resource`, `data`, `variable`, `provider`, etc.) intact.
* **YAML (Multi-Document & Structural):** Splittable along multi-document boundary separators (`---`), ensuring Kubernetes manifests or docker-compose definitions remain unified for optimal context cohesion.
* **Markdown (Heading-Based):** Slices sections cleanly at headings (`#`, `##`, `###`), organizing files topic-by-topic.
* **Edge-Case Safe Slicing:** Utilizes a custom rune-based overlap algorithm that handles multi-byte UTF-8 strings (emojis, international text) cleanly without boundary-corruption.

### 4. Automatic Session Memory Capture
* **Session Start Context Loading:** On startup, the extension queries DuckDB for relevant user preferences (e.g. *"User prefers functional style"*) and the active codebase's summaries, pre-loading them directly into the agent's context.
* **Session End Context Capture:** On shutdown, the extension reads the conversation transcript, uses LiteLLM to extract architectural decisions or newly expressed preferences, and persists them semantically to DuckDB.

---

## 🛠 Exposed MCP Tools

The extension exposes the following Model Context Protocol (MCP) tools:

* `search_memory`: Searches past memories, guidelines, or codebase profiles semantically.
* `add_memory`: Manually saves user preferences, code guidelines, or key project facts.
* `update_index`: Manually triggers an incremental update of the active workspace's Merkle tree index.
* `list_codebases`: Lists all indexed codebases on the system, including workspace paths and tree states.

---

## ⚙ Custom Terminal Command

* **/index `[path_to_directory]`**: Recursively scans, profiles, and indexes the target directory semantically. Once indexed, its memory profile is loaded automatically whenever you open a session inside that workspace!

---

## 🔧 Installation & Setup

1. **Build and Link (Install):**
   This compiles all Go binaries statically into `/dist` and links the extension in Gemini CLI:
   ```bash
   make install
   ```

2. **Configuration:**
   Configure the LiteLLM connection via standard CLI options or environment variables:
   * **Base URL:** `LITELLM_BASE_URL` (Defaults to `http://localhost:4000/v1`)
   * **Embedding Model:** `LITELLM_EMBEDDING_MODEL` (Defaults to `text-embedding-3-small`)
   * **Chat Model:** `LITELLM_CHAT_MODEL` (Defaults to `gpt-4o-mini`)

---

## 🧪 Testing & Verification

Run the unified package unit tests and the database self-checks:
```bash
go test ./... -v   # Run all package unit tests
make test          # Run local DuckDB parameter binding self-checks
```
