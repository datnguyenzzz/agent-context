# Gemini Persistent Memory & Codebase Indexer Extension

A model-agnostic Gemini CLI extension written in **Go** that provides persistent, long-term memory across sessions, and ultra-fast codebase indexing. Powered by **DuckDB** and **TurboQuant** for 12x-compressed, embedded vector similarity search.

---

## 🚀 Core Capabilities

### 1. Merkle Tree-Based Incremental Indexing
* **Cryptographic Diffing:** Builds SHA-256 hashes of the local codebase state. On subsequent scans, it diffs the new tree against the old state to isolate added, modified, and deleted files in milliseconds.
* **Redundant-Free Vectorization:** Skips calling the LLM embedding API for unchanged files.
* **Auto Garbage Collection:** Automatically purges stale vector chunks of deleted/modified files in DuckDB.

### 2. Multi-Codebase Architecture Portfolios
* **Auto-Profiling:** Summarizes directory structures into a global architectural profile for the agent's context.
* **Origin Tagging:** Searches clearly label results with source repositories and absolute paths:
  `[Codebase: agent-mem] [Path: /Users/thanh.nguyen/agent-mem] [Similarity: 88.5%]`
* **Discovery:** Exposes the `list_codebases` tool allowing the LLM to actively browse indexed workspaces.

### 3. TurboQuant 4-Bit Vector Compression
* **12x Storage Savings:** Automatically quantizes float32 embeddings to a compact **4-bit representation** inside DuckDB `BLOB` columns, reducing vector size from 6KB down to 768 bytes.
* **On-The-Fly Dequantization:** Decodes quantized BLOBs and scores them via Go-level cosine similarity in under a millisecond with virtually identical semantic fidelity (Cosine Sim > 0.98).

### 4. Compiler-Grade Semantic Splitters
* **Go (AST-Based):** Slices package-level declarations (`FuncDecl`, structs/interfaces, variables/constants). **Doc-comment aware**—includes preceding `/* */` and `//` block comments for rich context.
* **YAML (Structural):** Slices along multi-document boundary separators (`---`) for cohesive manifests.
* **Markdown (Heading-Based):** Slices logically at heading lines (`#`, `##`, `###`).
* **Terraform (Lexical Block-Based):** Groups logical configuration blocks intact.

---

## 🛠 Exposed MCP Tools

* `search_memory`: Searches past memories, guidelines, or codebase profiles semantically.
* `add_memory`: Manually saves user preferences, guidelines, or key project facts.
* `update_index`: Manually triggers an incremental update of the active codebase's index.
* `list_codebases`: Lists all indexed codebase paths and tree states on the system.

---

## 🔧 Installation & Setup

1. **Build and Link (Install):**
   ```bash
   make install
   ```

2. **Configuration Settings:**
   Configure via standard CLI options or environment variables:
   * **Base URL:** `LITELLM_BASE_URL` (Defaults to `http://localhost:4000/v1`)
   * **Embedding Model:** `LITELLM_EMBEDDING_MODEL` (Defaults to `text-embedding-3-small`)
   * **Chat Model:** `LITELLM_CHAT_MODEL` (Defaults to `gpt-4o-mini`)

---

## 🧪 Testing

```bash
go test ./... -v   # Run all package unit and live integration tests
make test          # Run DuckDB parameter-binding self-checks
```
