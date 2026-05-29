package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/manifoldco/promptui"
	"gopkg.in/yaml.v3"
)

// Config defines the execution parameters for the Stamis runtime.
type Config struct {
	Provider           string `yaml:"provider"`
	Model              string `yaml:"model"`
	Endpoint           string `yaml:"endpoint"`
	APIKeyEnv          string `yaml:"api_key_env"`
	TokenLimit         int    `yaml:"token_limit"`
	AutonomousLearning bool   `yaml:"autonomous_learning"`
	APIKey             string `yaml:"-"`
}

// Message represents a single chat turn in the runtime.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Name    string `json:"name,omitempty"`
}

// Global metrics and Security Gate
var lastMetrics Metrics
var sessionTokens int
var GlobalSecurityGate *SecurityGate

// loadEnv manually parses a local .env file natively to avoid external godotenv dependencies.
func loadEnv(filepath string) bool {
	file, err := os.Open(filepath)
	if err != nil {
		return false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) == 0 || strings.HasPrefix(line, "#") {
			continue
		}
		
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			
			// Strip trailing/leading quotes if the user wraps their secret
			val = strings.Trim(val, `"'`)
			os.Setenv(key, val)
		}
	}
	return true
}

func getEnvKeyForProvider(provider string) string {
	return strings.ToUpper(strings.TrimSpace(provider)) + "_API_KEY"
}

func checkOrRunSetup() {
	if _, err := os.Stat("config.yaml"); err == nil {
		return
	}

	fmt.Println("\n--- Stamis First-Time Setup ---")

	promptProvider := promptui.Select{
		Label: "Select Provider",
		Items: []string{"openrouter", "openai", "anthropic", "ollama", "local"},
	}
	_, provider, err := promptProvider.Run()
	if err != nil {
		log.Fatalf("Setup failed: %v", err)
	}

	commonModels := []string{
		"anthropic/claude-3-5-sonnet",
		"openai/gpt-4o",
		"google/gemini-1.5-pro",
		"meta-llama/llama-3-70b-instruct",
		"qwen3:8b",
		"Other",
	}

	promptModel := promptui.Select{
		Label: "Select Model Name",
		Items: commonModels,
	}
	_, model, err := promptModel.Run()
	if err != nil {
		log.Fatalf("Setup failed: %v", err)
	}

	if model == "Other" {
		promptOther := promptui.Prompt{
			Label: "Enter Custom Model Name",
		}
		model, err = promptOther.Run()
		if err != nil {
			log.Fatalf("Setup failed: %v", err)
		}
	}

	promptKey := promptui.Prompt{
		Label: "Enter API Key",
		Mask:  '*',
	}
	apiKey, err := promptKey.Run()
	if err != nil {
		log.Fatalf("Setup failed: %v", err)
	}

	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	apiKey = strings.TrimSpace(apiKey)

	endpoint := "http://localhost:11434/v1"
	providerClean := strings.ToLower(provider)
	if providerClean == "openrouter" {
		endpoint = "https://openrouter.ai/api/v1"
	} else if providerClean == "openai" {
		endpoint = "https://api.openai.com/v1"
	} else if providerClean == "anthropic" {
		endpoint = "https://api.anthropic.com/v1"
	}

	cfg := Config{
		Provider:           provider,
		Model:              model,
		Endpoint:           endpoint,
		TokenLimit:         4000,
		AutonomousLearning: true,
	}

	yamlData, _ := yaml.Marshal(&cfg)
	os.WriteFile("config.yaml", yamlData, 0644)

	envKey := getEnvKeyForProvider(provider)
	envContent := fmt.Sprintf("%s=%s\n", envKey, apiKey)
	os.WriteFile(".env", []byte(envContent), 0644)

	fmt.Println("[System] Setup complete. Your credentials have been secured.")
}

// loadConfig reads and parses the YAML configuration file.
func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if cfg.TokenLimit <= 0 {
		cfg.TokenLimit = 4000
	}

	providerClean := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if providerClean != "ollama" && providerClean != "local" {
		envKey := getEnvKeyForProvider(cfg.Provider)
		cfg.APIKey = os.Getenv(envKey)
		
		// Fallback for cloud/openrouter legacy configurations
		if cfg.APIKey == "" && (providerClean == "cloud" || providerClean == "openrouter") {
			if fallbackVal := os.Getenv("STAMIS_API_KEY"); fallbackVal != "" {
				cfg.APIKey = fallbackVal
				envKey = "STAMIS_API_KEY"
			} else if fallbackVal := os.Getenv("API_KEY"); fallbackVal != "" {
				cfg.APIKey = fallbackVal
				envKey = "API_KEY"
			}
		}

		if cfg.APIKey == "" {
			fmt.Printf("[Error] Provider '%s' requires environment variable '%s', but it is not set.\n", cfg.Provider, envKey)
			return nil, fmt.Errorf("missing environment variable %s", envKey)
		}
	} else {
		envKey := getEnvKeyForProvider(cfg.Provider)
		cfg.APIKey = os.Getenv(envKey)
	}

	return &cfg, nil
}

// buildSystemPrompt dynamically compiles the initial system message with available tools.
func buildSystemPrompt(config *Config) string {
	var systemPromptBuilder strings.Builder
	systemPromptBuilder.WriteString("You are 'Stamis', a bare-metal Agentic Operating System kernel. You are not a chatbot. You are a utility for task execution and environment management.\n")
	systemPromptBuilder.WriteString("Your tone must be strictly technical, neutral, and professional.\n")
	systemPromptBuilder.WriteString("Identity Protocol: If asked 'Who are you?', respond: 'Stamis Kernel v1.0. Role: Agentic Runtime. Status: Active.'\n")
	systemPromptBuilder.WriteString("Conversational Protocol: If input is non-task-oriented (greetings, 'thank you', casual chat), respond with a concise, neutral acknowledgment using plain text. Do not attempt to invoke any tools.\n")
	systemPromptBuilder.WriteString("Scope Protocol: If asked questions outside your utility (philosophy, opinions, general knowledge), respond: 'Request out of scope. I am a specialized agentic utility and cannot process this type of inquiry.'\n")
	systemPromptBuilder.WriteString("Enforcement: Ensure these rules are strictly observed by the LLM. It must NEVER attempt to use a tool to answer conversational or out-of-scope inquiries.\n")
	systemPromptBuilder.WriteString("Do not invent backstories, do not attempt to be 'friendly', and do not provide 'creative' explanations. Your purpose is high-integrity execution of registered skills.\n")
	systemPromptBuilder.WriteString("Goal: Direct, concise, and focused on tool orchestration.\n")

	tools := LoadSkills("skills")
	if len(tools) > 0 {
		systemPromptBuilder.WriteString("\nYou have access to the following tools:\n")
		
		hasLogLearning := false
		for i, t := range tools {
			systemPromptBuilder.WriteString(fmt.Sprintf("%d. %s: %s\n", i+1, t.Function.Name, t.Function.Description))
			if t.Function.Name == "log_learning" {
				hasLogLearning = true
			}
		}

		if config.AutonomousLearning && hasLogLearning {
			systemPromptBuilder.WriteString("\nBRAIN LOG ACTIVE: You possess a 'Brain Log'. After every successful task, call the [log_learning] tool to reflect on what you learned (errors, corrections, or better approaches) so you can avoid mistakes in the future.\n")
		}

		systemPromptBuilder.WriteString("\nIMPORTANT: If you need to fetch real-time data or use a tool, you MUST output exactly: <TOOLCALL>[tool_name(args)]</TOOLCALL>\nDo NOT refuse. Do NOT output a conversational refusal.")
	}

	return systemPromptBuilder.String()
}

// askForConfirmation securely prompts the user before executing shell commands.
func askForConfirmation(prompt string, scanner *bufio.Scanner) bool {
	fmt.Printf("\n[Safety System] %s (y/n): ", prompt)
	if scanner.Scan() {
		answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
		return answer == "y" || answer == "yes"
	}
	return false
}

// handleSlashCommand intercepts and executes agentic local commands.
func handleSlashCommand(input string, history *[]Message, config *Config, scanner *bufio.Scanner) bool {
	if !strings.HasPrefix(input, "/") {
		return false
	}

	parts := strings.SplitN(input, " ", 3)
	command := parts[0]

	switch command {
	case "/clear":
		*history = []Message{
			{Role: "system", Content: buildSystemPrompt(config)},
		}
		fmt.Println("[System] Context memory completely flushed. Tools rescanned.")

	case "/status":
		fmt.Println("\n=== STAMIS RUNTIME STATUS ===")
		fmt.Printf("Active Provider: %s\n", config.Provider)
		fmt.Printf("Endpoint: %s\n", config.Endpoint)
		fmt.Printf("Model: %s\n", config.Model)
		fmt.Printf("Session Tokens Processed: ~%d\n", sessionTokens)
		if lastMetrics.TotalLatency > 0 {
			fmt.Printf("Last Request TTFT: %v\n", lastMetrics.TTFT)
			fmt.Printf("Last Request Total Latency: %v\n", lastMetrics.TotalLatency)
		}
		fmt.Println("=============================")

	case "/provider":
		if len(parts) < 2 {
			fmt.Println("[Error] Usage: /provider [local|cloud]")
			return true
		}
		target := strings.ToLower(parts[1])
		if target == "local" || target == "ollama" {
			config.Provider = "local"
			config.Endpoint = "http://localhost:11434/v1"
			config.Model = "qwen3:8b" // Optional default fallback
			fmt.Println("[System] Switched to Local execution engine (Zero-Latency).")
		} else if target == "cloud" {
			config.Provider = "cloud"
			config.Endpoint = "https://openrouter.ai/api/v1"
			envKey := getEnvKeyForProvider(config.Provider)
			config.APIKey = os.Getenv(envKey)
			if config.APIKey == "" {
				if fallbackVal := os.Getenv("STAMIS_API_KEY"); fallbackVal != "" {
					config.APIKey = fallbackVal
				} else if fallbackVal := os.Getenv("API_KEY"); fallbackVal != "" {
					config.APIKey = fallbackVal
				}
			}
			fmt.Println("[System] Switched to Cloud execution engine (OpenRouter).")
		} else {
			fmt.Println("[Error] Unknown provider type. Use 'local' or 'cloud'.")
		}

	case "/read":
		if len(parts) < 2 {
			fmt.Println("[Error] Usage: /read [filepath]")
			return true
		}
		filepath := parts[1]
		data, err := os.ReadFile(filepath)
		if err != nil {
			fmt.Printf("[Error] Failed to read file: %v\n", err)
			return true
		}
		fileContent := fmt.Sprintf("[Observation: Read file '%s']\n%s", filepath, string(data))
		*history = append(*history, Message{Role: "user", Content: fileContent})
		fmt.Printf("[System] Injected %d bytes from %s into context.\n", len(data), filepath)
		sessionTokens += estimateTokens(string(data))

	case "/write":
		if len(parts) < 3 {
			fmt.Println("[Error] Usage: /write [filepath] [content]")
			return true
		}
		filepath := parts[1]
		content := parts[2]
		err := os.WriteFile(filepath, []byte(content), 0644)
		if err != nil {
			fmt.Printf("[Error] Failed to write file: %v\n", err)
			return true
		}
		actionMsg := fmt.Sprintf("[Action: Wrote content to '%s']", filepath)
		*history = append(*history, Message{Role: "system", Content: actionMsg})
		fmt.Printf("[System] Successfully wrote to %s.\n", filepath)

	case "/exec":
		if len(parts) < 2 {
			fmt.Println("[Error] Usage: /exec [command]")
			return true
		}
		cmdString := strings.TrimSpace(input[6:]) // safely extract the rest of the string
		
		if !askForConfirmation(fmt.Sprintf("Execute local shell command: '%s'?", cmdString), scanner) {
			fmt.Println("[System] Command execution aborted.")
			return true
		}

		cmd := exec.Command("cmd", "/C", cmdString)
		output, err := cmd.CombinedOutput()
		
		outStr := string(output)
		if err != nil {
			outStr += fmt.Sprintf("\n[Process exited with error: %v]", err)
		}
		
		fmt.Printf("\n--- Command Output ---\n%s\n----------------------\n", outStr)
		
		contextMsg := fmt.Sprintf("[Action: Executed '%s']\n[Output]:\n%s", cmdString, outStr)
		*history = append(*history, Message{Role: "system", Content: contextMsg})
		sessionTokens += estimateTokens(outStr)

	default:
		fmt.Printf("[Error] Unknown slash command: %s\n", command)
	}

	return true
}

func main() {
	checkOrRunSetup()

	// Automated Initialization Routine: check and create .env if missing
	if _, err := os.Stat(".env"); os.IsNotExist(err) {
		templateStr := "# Stamis Configuration\n# Set your provider-specific key below (e.g., ANTHROPIC_API_KEY or OPENROUTER_API_KEY)\nAPI_KEY=\n"
		err := os.WriteFile(".env", []byte(templateStr), 0644)
		if err == nil {
			fmt.Println("[System] Initialized: .env file created. Please add your API key.")
		}
	}

	InitTelemetry()
	
	// Step 1: Professional Secret Management
	if loadEnv(".env") {
		fmt.Println("[System] Config loaded: .env file detected")
	} else {
		fmt.Println("[System] Using system environment variables")
	}

	// Step 2: Initialize Security Kernel
	GlobalSecurityGate = NewSecurityGate("security_policy.yaml")

	configPath := "config.yaml"
	config, err := loadConfig(configPath)
	if err != nil {
		log.Fatalf("Fatal: %v\n", err)
	}

	fmt.Printf("Stamis Runtime Initialized.\nProvider: %s\nModel: %s\nEndpoint: %s\nToken Limit: %d\n", config.Provider, config.Model, config.Endpoint, config.TokenLimit)

	tools := LoadSkills("skills")
	hasLogLearning := false
	for _, t := range tools {
		if t.Function.Name == "log_learning" {
			hasLogLearning = true
			break
		}
	}
	
	if config.AutonomousLearning && hasLogLearning {
		fmt.Println("[System] Brain Log Enabled: Agent is in Autonomous Learning mode.")
	}
	fmt.Println()

	history := []Message{
		{Role: "system", Content: buildSystemPrompt(config)},
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\nShutdown signal received. Exiting Stamis.")
		os.Exit(0)
	}()

	scanner := bufio.NewScanner(os.Stdin)

	for {
		fmt.Print("\nstamis> ")
		if !scanner.Scan() {
			break
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		if strings.ToLower(input) == "exit" || strings.ToLower(input) == "quit" {
			fmt.Println("Exiting Stamis.")
			break
		}

		// Intercept Slash Commands
		if handleSlashCommand(input, &history, config, scanner) {
			continue
		}

		history = append(history, Message{Role: "user", Content: input})
		sessionTokens += estimateTokens(input)

		// Autonomous Sub-Loop for Tool-Calling Chain
		for {
			// Dynamically rebuild history[0] to ensure context is always fresh
			if len(history) > 0 && history[0].Role == "system" {
				history[0].Content = buildSystemPrompt(config)
			}
			
			optimizedHistory := OptimizeHistory(history, config.TokenLimit)
			
			// Log telemetry for active skills
			if len(ActiveSkills) > 0 {
				EmitTelemetry("tools_loaded", len(ActiveSkills))
			}

			fmt.Print("\n") // Format output spacing before the stream starts

			response, err := SendChatRequest(config, optimizedHistory, os.Stdout, &lastMetrics)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\n[Stream Error]: %v\n", err)
				break
			}
			
			fmt.Print("\n") // Ensure spacing after the stream completes

			history = append(history, *response)
			sessionTokens += estimateTokens(response.Content)

			// Process Custom Text-Based Tool Calls Interception
			if strings.Contains(response.Content, "<TOOLCALL>") {
				fmt.Print("[System] Intercepting Tool Call...\n")
				content := response.Content
				toolExecuted := false

				for {
					startIdx := strings.Index(content, "<TOOLCALL>")
					if startIdx == -1 {
						break
					}
					content = content[startIdx+len("<TOOLCALL>"):]
					
					endIdx := strings.Index(content, "</TOOLCALL>")
					if endIdx == -1 {
						break
					}
					
					toolData := content[:endIdx]
					content = content[endIdx+len("</TOOLCALL>"):]

					// Clean payload
					toolData = strings.Trim(toolData, "[] \n\r\t")
					
					var toolName, toolArgs string
					if parenIdx := strings.Index(toolData, "("); parenIdx != -1 {
						toolName = strings.TrimSpace(toolData[:parenIdx])
						toolArgs = strings.TrimSpace(toolData[parenIdx+1 : strings.LastIndex(toolData, ")")])
					} else {
						toolName = strings.TrimSpace(toolData)
					}

					skillDef, ok := ActiveSkills[toolName]
					var toolResult string
					
					if !ok {
						toolResult = fmt.Sprintf("Error: Skill '%s' not found.", toolName)
					} else {
						// Step 3: Route execution through the SecurityGate
						authResult := GlobalSecurityGate.Authorize(toolName, skillDef.Command)
						
						if authResult == DENIED {
							toolResult = "Security Rejection: Access Denied"
							fmt.Printf("-> Tool '%s' Execution BLOCKED by SecurityGate.\n", toolName)
						} else {
							cmdStr := fmt.Sprintf("%s '%s'", skillDef.Command, toolArgs)
							
							if authResult == AUDIT_REQUIRED {
								cmdStr = "sentinel run -- " + cmdStr
							}
							
							cmd := exec.Command("cmd", "/C", strings.TrimSpace(cmdStr))
							out, err := cmd.CombinedOutput()
							
							toolResult = string(out)
							if err != nil {
								toolResult += fmt.Sprintf("\n[Error: %v]", err)
							}
							fmt.Printf("-> Tool '%s' Output: %s\n", toolName, strings.TrimSpace(toolResult))
						}
					}

					EmitTelemetry("text_tool_result", map[string]string{"tool": toolName, "result": toolResult})

					history = append(history, Message{
						Role:    "system",
						Content: fmt.Sprintf("[System/ToolResult for %s]:\n%s", toolName, toolResult),
					})
					sessionTokens += estimateTokens(toolResult)
					toolExecuted = true
				}

				if toolExecuted {
					continue // Automatically recurse to let LLM analyze the local tool result
				}
			}

			// Break out of the autonomous loop if no tools were called
			break
		}
	}

	if err := scanner.Err(); err != nil {
		log.Fatalf("Input error: %v", err)
	}
}