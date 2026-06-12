---
title: Native Windows Support
slug: windows-support
priority: P4
status: done
spec: complete
code: done
package: internal/tool
depends_on:
  - tool-registry
  - bash
  - dangerous-command-gate
---

# Native Windows Support

## Overview

Jenny provides native Windows support through harness-driven tooling. The environment automatically determines the available toolset, ensuring a seamless and secure experience on Windows without requiring the Agent to manage platform differences.

## 1. Core Principles: Environment-Driven Tooling

- **Platform Awareness**: The `Registry` automatically detects the operating system and registers the appropriate shell tool (`BashTool` for Unix, `PowerShellTool` for Windows).
- **Tool Visibility**: On Windows, `BashTool` is only registered if a compatible bash environment (e.g., Git Bash) is detected in the `PATH`.
- **Semantic Consistency**: Tools are named according to their function and shell. LLMs will naturally adapt their syntax based on the tool available (e.g., using PowerShell syntax when the `PowerShell` tool is present).

## 2. Architectural Components

### 2.1 PowerShellTool (`internal/tool/powershell.go`)

A new tool implementation specifically for Windows PowerShell.
- **Execution**: `powershell.exe -NoProfile -NonInteractive -Command "..."`.
- **Encoding**: Force UTF-8 output to prevent encoding issues common with Windows legacy locales (e.g., GBK).
- **Lifecycle Management**: Use `exec.CommandContext` for reliable process tree termination on Windows.
- **Background Support**: Parallel implementation of background task management similar to `BashTool`.

### 2.2 Windows Security Gateway (`internal/tool/gate_windows.go`)

A specialized version of `CommandGate` tailored for Windows security conventions.
- **Path Restriction**: Block access to critical system paths:
    - `C:\Windows\System32`
    - `C:\Users\*\AppData`
    - `C:\$Recycle.Bin`
- **Command Interception**: Prevent execution of sensitive administrative commands:
    - `Set-ExecutionPolicy`, Registry modifications (`reg.exe`), Service management (`sc.exe`).
- **Device Protection**: Block access to named pipes (`\\.\pipe\`) and raw physical drives.

### 2.3 Intelligent Registry (`internal/tool/registry.go`)

Update the `Registry.Build()` method to include platform-specific logic:
- Register `PowerShellTool` on Windows.
- Conditionally register `BashTool` on Windows if `bash.exe` is found.

## 3. Cross-Platform Refinement

### 3.1 Path Handling
- **Home Directory**: Transition from `os.Getenv("HOME")` to `os.UserHomeDir()` for reliable cross-platform home path resolution.
- **Path Validation**: Enhance `isPathWithinCwd` to correctly handle Windows drive letters and case-insensitive path comparisons.

### 3.2 Signal and Process Management
- **Task Termination**: Replace Unix-specific `syscall.SIGTERM` with `process.Kill()` or `taskkill /F /T /PID <pid>` on Windows.

### 3.3 Dynamic System Prompt
Adapt `internal/agent/prompt.go` to include platform-specific hints:
- **Windows Hint**: "You are running on Windows. Use the PowerShell tool for system commands. Be aware of Windows file path conventions (e.g., C:\path\to\file)."

## 4. Acceptance Criteria

- **AC1**: `Registry` correctly identifies Windows and registers `PowerShellTool`.
- **AC2**: `PowerShellTool` executes commands and returns UTF-8 encoded output.
- **AC3**: `WindowsCommandGate` blocks access to `System32` and administrative commands.
- **AC4**: Path resolution uses `os.UserHomeDir()` and handles drive letters.
- **AC5**: Background tasks on Windows are reliably terminated via process tree killing.
- **AC6**: GitHub Actions `windows-latest` workflow passes build and tests.

## 5. Implementation Milestones

1. **Milestone 1 (Foundation)**: Refactor path resolution and constants to use `os.UserHomeDir()`.
2. **Milestone 2 (Implementation)**: Develop `PowerShellTool` and `WindowsCommandGate`.
3. **Milestone 3 (Integration)**: Update `Registry` for platform-aware tool registration.
4. **Milestone 4 (Validation)**: Document and verify the `windows-latest` GitHub Actions workflow.
