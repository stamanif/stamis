package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type SecurityPolicy struct {
	AllowedProcessLineage []string `yaml:"allowed_process_lineage"`
	ProtectedPaths        []string `yaml:"protected_paths"`
	PrivilegedTools       []string `yaml:"privileged_tools"`
}

type AuthResult int

const (
	ALLOWED AuthResult = iota
	AUDIT_REQUIRED
	DENIED
)

type SecurityGate struct {
	Policy         *SecurityPolicy
	PermissiveMode bool
}

func NewSecurityGate(policyPath string) *SecurityGate {
	gate := &SecurityGate{
		Policy: &SecurityPolicy{},
	}

	data, err := os.ReadFile(policyPath)
	if err == nil {
		yaml.Unmarshal(data, gate.Policy)
	}

	// Daemon Resilience: Check if sentinel daemon is running with a non-blocking timeout
	sentinelPath, err := exec.LookPath("sentinel")
	if err != nil {
		fmt.Println("[System] Sentinel Daemon offline: Running in Permissive Mode")
		gate.PermissiveMode = true
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, sentinelPath, "ping")
		if err := cmd.Run(); err != nil {
			fmt.Println("[System] Sentinel Daemon offline: Running in Permissive Mode")
			gate.PermissiveMode = true
		}
	}

	return gate
}

func (sg *SecurityGate) Authorize(toolName, commandStr string) AuthResult {
	// Fast Path: Check if tool is privileged
	isPrivileged := false
	for _, pt := range sg.Policy.PrivilegedTools {
		if toolName == pt {
			isPrivileged = true
			break
		}
	}

	if !isPrivileged {
		return ALLOWED
	}

	// Process Validation: Lineage Check
	if len(sg.Policy.AllowedProcessLineage) > 0 {
		cmdStr := fmt.Sprintf("Get-Process -Id %d | Select-Object -ExpandProperty Parent", os.Getpid())
		cmd := exec.Command("powershell", "-Command", cmdStr)
		out, err := cmd.CombinedOutput()
		
		if err != nil {
			EmitTelemetry("SECURITY_VIOLATION", fmt.Sprintf("Lineage check failed for tool %s: %v", toolName, err))
			return DENIED
		}

		parentInfo := strings.ToLower(string(out))
		lineageMatch := false
		for _, allowed := range sg.Policy.AllowedProcessLineage {
			if strings.Contains(parentInfo, strings.ToLower(allowed)) {
				lineageMatch = true
				break
			}
		}

		if !lineageMatch {
			EmitTelemetry("SECURITY_VIOLATION", fmt.Sprintf("Process lineage %s not in allowed list for tool %s", strings.TrimSpace(parentInfo), toolName))
			return DENIED
		}
	}

	if sg.PermissiveMode {
		return ALLOWED
	}

	return AUDIT_REQUIRED
}
