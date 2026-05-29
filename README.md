# Stamis

Stamis is a bare-metal Agentic Operating System kernel designed for task execution and environment management. It operates with high-integrity tool orchestration, featuring built-in telemetry, an interactive setup wizard, security governance, and multi-provider AI support.

## Features

- **Interactive Setup Wizard**: Seamlessly configures your preferred AI provider, model, and API keys via an interactive terminal menu.
- **Provider Agnostic**: Works out-of-the-box with OpenRouter, Anthropic, OpenAI, Ollama, and Local execution engines.
- **Security Governance**: Implements a `SecurityGate` with policies to audit and restrict tool usage.
- **Slash Commands**: Control the runtime directly using local commands (e.g., `/clear`, `/status`, `/provider`, `/exec`).
- **Autonomous Learning**: Brain Log feature enables the agent to learn from successful tasks to avoid future mistakes.

## Prerequisites

Before running Stamis, you must install **Go** on your system.
1. Download and install Go from the official website: [https://go.dev/dl/](https://go.dev/dl/)
2. Verify the installation by opening a terminal and running:
   ```bash
   go version
   ```

## Installation

1. **Clone the repository:**
   ```bash
   git clone https://github.com/stamanif/stamis.git
   cd stamis
   ```

2. **Download Dependencies:**
   Run the following command to download the required Go modules (like `promptui` and `yaml`):
   ```bash
   go mod tidy
   ```

3. **Build the Binary:**
   Compile the source code into an executable:
   ```bash
   go build -o stamis.exe .
   ```
   *(On macOS/Linux, the command is `go build -o stamis .`)*

## Usage

Start the Stamis runtime by running the built binary:

```bash
# On Windows
.\stamis.exe

# On macOS/Linux
./stamis
```

### First-Time Setup
If this is your first time running Stamis, an interactive wizard will appear. 
- Use your **Up/Down arrow keys** to select your preferred AI **Provider** and **Model**.
- Type your **API Key** when prompted (your typing will be securely masked).

The wizard will automatically generate your `config.yaml` and `.env` files and securely store your credentials.

### Slash Commands
Once in the `stamis>` prompt, you can type messages or use the following system commands:
- `/status` - View current runtime status, active provider, and token metrics.
- `/provider [local|cloud]` - Hot-swap between a local engine (e.g., Ollama) and a cloud provider.
- `/clear` - Flush the context memory and rescan tools.
- `/exec [command]` - Execute a local shell command with safety confirmation.
- `/read [filepath]` - Inject file contents into the context memory.
- `/write [filepath] [content]` - Write content to a specified file.

To exit the runtime, type `exit` or `quit`.

## Configuration Files

- `config.yaml`: Stores your execution parameters (Provider, Model, Endpoint, Token Limit).
- `.env`: Stores your sensitive secrets (e.g., `OPENROUTER_API_KEY`). **Do not commit this file to GitHub.**
- `security_policy.yaml`: Defines the security policies, allowed tool lineages, and paths.

## License
MIT License
