# Jenny WebUI Portal Architecture

## 1. Overview & Rationale

Jenny is designed to be a pure, headless CLI agent. While the CLI excels at focused, atomic task execution, it inherently lacks the visual bandwidth to effectively display multi-dimensional data such as long-term cost trends, complex MCP topologies, or deep conversation histories.

To solve this without compromising the core CLI experience or introducing a heavy Terminal User Interface (TUI), we are introducing the **Jenny WebUI Portal**. The Portal acts as an out-of-band "Command Center"—a lightweight, sidecar HTTP server that provides a rich visual interface for configuration, monitoring, and session management, while leaving the core agent loop untouched.

## 2. Core Architectural Principles

### 2.1 The "Sidecar" Observer Model
The `jenny` core process remains entirely unaware of the Portal. The Portal functions purely as a file-system observer and a process launcher. 

*   **No Database:** We strictly avoid SQLite or heavy databases to minimize binary bloat. The file system (`~/.jenny/sessions`) is the single source of truth.
*   **UUID v7 as Index:** By utilizing UUID v7 for session IDs, directories are naturally time-sorted. The Portal can list sessions chronologically simply by running a directory read, without needing to store or parse timestamps.
*   **State Derivation:** Session states (Running vs. Exited) are derived dynamically. The Portal reads the `pid` file within a session directory and checks process liveness (e.g., via signal 0) rather than relying on the session to explicitly report its status.

### 2.2 Headless Interaction Flow
Because `jenny` operates in a headless batch mode (accepting an initial prompt and running until completion without standard input interaction), the Portal's interaction model is simplified:

*   **Start:** Creating a new session via the WebUI triggers the Portal to spawn a detached `jenny -p "..."` process.
*   **Observe:** The WebUI receives updates via Server-Sent Events (SSE). The Portal tails the active session's transcript file and streams the events to the browser.
*   **Resume:** Resuming a session means appending a new prompt to an exited session. The Portal spawns a new detached process via `jenny -r <uuid> -p "<new_prompt>"`.

## 3. UI Structure & Layout

The frontend is a Single Page Application (SPA) built with Vite and React, embedded directly into the Jenny binary. It supports internationalization (i18n) for global accessibility.

The UI follows a Master-Detail layout optimized for observability:

1.  **Dashboard (Start):**
    *   Centralized prompt input for launching new sessions.
    *   Contextual configuration for Working Directory, Model Profiles, and global settings.
    *   Aggregated metrics: Active sessions, total token cost, and cache efficiency.
2.  **Sessions View:**
    *   **Master List:** A chronological sidebar of sessions with status indicators (Running/Exited).
    *   **Detail View:** A rich transcript renderer. It displays the conversation stream, tool execution logs, thinking process, and cost per turn.
    *   **Control Bar:** Contextual actions like "Stop" (SIGTERM), "Delete", and "New Prompt" (Resume).
3.  **Projects View:**
    *   Grouped views based on working directories.
    *   Project-level configurations (e.g., `AGENTS.md` editing) and aggregated cost reports.
4.  **Marketplace & Asset Management:**
    *   Unified interface for managing globally installed Skills, MCPs, and Plugins.
    *   Browse and install new capabilities from remote repositories.

## 4. Security & Lifecycle Management

*   **Localhost Binding:** The HTTP server binds exclusively to `127.0.0.1`.
*   **Dynamic High-Port & Lockfile:** The Portal binds to a random high port. Connection details (Port, PID, and a randomly generated Auth Token) are written to `~/.jenny/portal.lock`.
*   **Token Authentication:** All API requests and SSE streams must include the Auth Token, preventing unauthorized access from other local applications.
*   **Auto-Exit (Ephemeral Lifetime):** The Portal shuts down when inactive to save resources.
    *   The browser sends periodic heartbeat pings via HTTP.
    *   If no heartbeat is received for a set duration (e.g., 10 minutes), the Portal server cleans up the lockfile and exits.
*   **Single Instance Enforcement:** Launching the Portal checks for an existing lockfile. If found and the process is alive, it opens the browser to the active session and exits.

## 5. Deployment

The WebUI assets are bundled and injected into the Go binary using `//go:embed`, maintaining the "single executable" philosophy of Jenny.
