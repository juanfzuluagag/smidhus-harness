# Smidhus Harness
> **Official Website:** [smidhus.dev](https://smidhus.dev) | **Documentation:** [docs.smidhus.dev](https://docs.smidhus.dev)

Smidhus Harness is a lightweight, local orchestrator and deterministic execution engine designed for Spec-Driven Development (SDD). Built in Go, it serves as a central control plane to coordinate a team of autonomous AI agents through the OpenCode platform.

Traditional agentic workflows often suffer from infinite loops, agent stalling, and uncontrolled context expansion when agents try to orchestrate their own execution. Smidhus Harness solves this by managing the state machine natively in compiled Go code. The AI agents focus purely on execution (design, code, cloud, QA, docs) while Go enforces the transitions, handles timeouts, and monitors execution health.

By offloading state machine logic, directory tree parsing, and loop management from the LLM context to local Go execution, Smidhus Harness drastically reduces token consumption and eliminates hallucinated infinite loops *(yielding an estimated 40% to 60% token savings on complex, long-term projects)*. 

While the savings on trivial single-shot tasks may be negligible, the architectural advantage becomes massive during deep development cycles. Traditional autonomous agents suffer from **"context bloat"** (the history tax): every failed attempt, shell error, and role-switch is appended to a continuously growing context window that you pay for on every subsequent turn. Smidhus solves this by completely isolating and resetting the context between phases. A `Builder` agent spawns with a clean slate, reading only the finalized Markdown specs, without having to load the entire conversational history of the `Architect`. You only spend tokens on what matters: generating actual code, rather than paying an AI to parse its own past mistakes.

---

## Key Features

*   **Deterministic State Machine:** Go manages the workflow state machine natively, preventing agents from getting stuck in loops.
*   **Smart Fail-Fast Guard:** Real-time quota and API monitoring intercepts execution immediately upon encountering rate limits (HTTP 429/quota exhaustion), preventing prolonged hangs and unnecessary billing.
*   **Vision Integration for UI/UX:** The Designer agent scans visual mockups dropped into the specs directory to generate pixel-perfect, responsive components.
*   **Clean Workspace Management:** The Gatekeeper archives successfully validated specification files automatically upon test completion to avoid cluttering the developer's workspace.
*   **Dark Forge Style CLI:** A serious, industrial, high-performance console interface optimized for engineering teams.

---

## The AI Agents Pool

Smidhus Harness coordinates 6 specialized roles. Each agent should be paired with the appropriate model type in your configuration:

1.  **Architect**
    *   *Purpose:* Translates business tasks into technical specifications and requirements. Defines the required development agents.
    *   *Model Requirement:* High-reasoning model (e.g., thinking models) to ensure accurate technical planning and constraint design.
2.  **Builder**
    *   *Purpose:* The main software developer. Implements the backend logic and writes unit tests first.
    *   *Model Requirement:* Advanced coding/reasoning model.
3.  **Designer**
    *   *Purpose:* Frontend UI/UX engineer. Focuses on HTML/CSS, Tailwind, and components.
    *   *Model Requirement:* Vision-capable model to parse mockup images and layout instructions.
4.  **Cloud**
    *   *Purpose:* Infrastructure as Code (IaC) and DevOps specialist. Manages Docker files, CI/CD pipelines, and cloud scripts.
    *   *Model Requirement:* Standard to high reasoning model with knowledge of IaC tooling.
5.  **Gatekeeper**
    *   *Purpose:* QA and auditor. Runs the verification pipeline (linting, tests, build checks).
    *   *Model Requirement:* Highly rigorous reasoning model to analyze test outputs and identify bugs.
6.  **Documenter**
    *   *Purpose:* Technical writer. Updates READMEs, APIs, and Architecture Decision Records (ADRs).
    *   *Model Requirement:* Fast, standard text generation model.

---

## Prerequisites

Ensure you have the following installed on your machine:
*   **Go** (version 1.21 or higher)
*   **OpenCode CLI** (`https://opencode.ai`)
*   **API Keys** for your chosen AI providers (configured within the OpenCode environment)

---

## Installation

Clone the repository and build the binary:

```bash
git clone https://github.com/juanfzuluagag/smidhus-harness.git
cd smidhus-harness
go build -o smidhus-harness ./cmd/harness
```

Or install it directly via Go:

```bash
go install github.com/juanfzuluagag/smidhus-harness/cmd/harness@latest
```

---

## Usage Guide (The Development Loop)

### Step 1: Initialize the Project
Run the initialization command in your target project directory:

```bash
smidhus-harness init
```

During this step, the CLI will:
1. Scan your project structure (or run an interactive questionnaire if it's a blank project).
2. Generate the **Project Manifest** (`.harness/blueprint.md`), establishing the tech stack, architectures, security protocols, and testing commands.
3. Generate the **Agents Configuration** (`.harness/agents.yml`).
4. Seed the initial state machine files in `.harness/state/`.

### Step 2: Configure Agents and Models
Open `.harness/agents.yml` to define which model runs each agent. Pair complex roles with reasoning engines and standard roles with fast models:

```yaml
global_settings:
  timeout: 900
  thinking_budget_ms: 2000

agents:
  architect:
    model: "deepseek-v4-pro"
  builder:
    model: "nvidia/qwen3-next-80b-a3b-thinking"
  gatekeeper:
    model: "google/gemini-2.5-pro"
  cloud:
    model: "google/gemini-2.5-flash"
  designer:
    model: "google/gemini-2.5-flash" # Vision support is critical here
  documenter:
    model: "google/gemini-2.5-flash"
```

### Step 3: Define Tasks
Define your development roadmap in `.harness/state/tasks.json`. Add tasks following this structure:

```json
{
  "project": "my-web-app",
  "tasks": [
    {
      "id": "T001",
      "title": "Configure authentication middleware with JWT token validation",
      "status": "pending"
    }
  ]
}
```

### Step 4: Run the Orchestrator
To kick off the automated loop, run:

```bash
smidhus-harness run
```

The orchestrator will execute the following automated pipeline:

```
[ pending ] ──> ( Architect Runs ) ──> [ spec_ready ] ──> [ HUMAN APPROVAL ]
                                                                 │
[ reviewing ] <── ( Builder/Designer/Cloud ) <── [ approved / in_progress ]
     │
     └── ( Gatekeeper Passes ) ──> [ documenting ] ──> ( Documenter ) ──> [ done ]
```

1.  **Pending:** The `Architect` generates requirements and technical designs under `.harness/specs/` and selects the `required_agents` (e.g., `["builder"]` or `["designer", "builder"]`).
2.  **Spec Ready [Human Gate]:** The orchestrator pauses. Review the generated specs in `.harness/specs/` and change the status in `tasks.json` to `"approved"` to resume.
3.  **Approved / In Progress:** The orchestrator runs the assigned development agents sequentially.
4.  **Reviewing:** The `Gatekeeper` runs validation commands (lint, test, build). If they fail, errors are reported to the Builder to retry. If they pass, specs are archived.
5.  **Documenting:** The `Documenter` updates project readmes and APIs.
6.  **Done:** The task is marked complete, and the orchestrator moves to the next pending task.

---

## Visual Mockups Integration

For UI tasks (e.g., frontend components or styling), you can provide design files for the **Designer** agent. 

Simply drop a mockup image inside the `.harness/specs/` directory named exactly after the task ID:
*   `.harness/specs/T003_mockup.png`
*   `.harness/specs/T003_mockup.jpg`

When the Designer agent runs on task `T003`, it will automatically detect the mockup, analyze the layout visually, and implement pixel-perfect frontend code in the target project.

---

## AI Skills & MCP Plugins

Smidhus Harness supports dynamic skill provisioning for agents using a strictly declarative, auto-equip architecture. Developers do not need to manually run plugin installation commands on their local environments; instead, they declare the dependencies inside the `.harness/agents.yml` file.

To equip an agent with specific skills or Model Context Protocol (MCP) servers, add the `skills` list configuration under that agent:

```yaml
global_settings:
  timeout: 900
  thinking_budget_ms: 2000

agents:
  builder:
    model: "nvidia/qwen3-next-80b-a3b-thinking"
  cloud:
    model: "google/gemini-2.5-flash"
    skills:
      - "aws/cli-manager"
      - "github/repo-manager"
```

Before invoking the agent, the harness automatically resolves and installs the declared skills locally using OpenCode's native installation mechanism (`opencode plugin <skill>`). If any skill fails to install, execution stops immediately to avoid run errors.

> [!NOTE]
> **MCP & Skill Configuration:** Connection strings, API tokens, or server-specific parameters (e.g., PostgreSQL credentials) are configured directly in your local system shell (as environment variables) or via OpenCode's configuration. Smidhus Harness does not store or process credentials; it simply inherits your active shell session's environment when invoking the tools. Please refer to the specific MCP/skill documentation for its configuration requirements.

---

## License

This project is free and open-source software distributed under the **GNU General Public License v3.0 (GPLv3)**. 

This means you are free to use, modify, and distribute this software. However, any derivative works or modifications must also be released as open-source under the same GPLv3 license, ensuring that the core engine remains free and open for the community forever. 

See the `LICENSE` file for more details.
