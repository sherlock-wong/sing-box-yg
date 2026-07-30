package platform

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

type Command struct {
	Path    string
	Args    []string
	Env     []string
	Timeout time.Duration
	Redact  []string
}

type CommandRunner interface {
	Run(context.Context, Command) (CommandResult, error)
}
type SystemRunner struct{}

func (SystemRunner) Run(ctx context.Context, command Command) (CommandResult, error) {
	return Run(ctx, command)
}

type RunnerFunc func(context.Context, Command) (CommandResult, error)

func (function RunnerFunc) Run(ctx context.Context, command Command) (CommandResult, error) {
	return function(ctx, command)
}

type CommandResult struct {
	Output   string
	ExitCode int
}

type CommandError struct {
	Command Command
	Result  CommandResult
	Cause   error
}

func (err *CommandError) Error() string {
	return fmt.Sprintf("command %s failed (exit %d): %v", displayCommand(err.Command), err.Result.ExitCode, err.Cause)
}
func (err *CommandError) Unwrap() error { return err.Cause }

// Run always has a finite timeout, captures combined output, and keeps listed
// secret values out of diagnostics.
func Run(parent context.Context, command Command) (CommandResult, error) {
	if command.Path == "" {
		return CommandResult{}, fmt.Errorf("command path is required")
	}
	if command.Timeout <= 0 {
		command.Timeout = 30 * time.Second
	}
	executionContext, cancel := context.WithTimeout(parent, command.Timeout)
	defer cancel()
	execution := exec.CommandContext(executionContext, command.Path, command.Args...)
	if len(command.Env) > 0 {
		execution.Env = append(os.Environ(), command.Env...)
	}
	output, err := execution.CombinedOutput()
	result := CommandResult{Output: redact(string(output), command.Redact), ExitCode: 0}
	if err == nil {
		return result, nil
	}
	result.ExitCode = -1
	if exitError, ok := err.(*exec.ExitError); ok {
		result.ExitCode = exitError.ExitCode()
	}
	if executionContext.Err() == context.DeadlineExceeded {
		err = fmt.Errorf("timed out after %s", command.Timeout)
	}
	return result, &CommandError{Command: command, Result: result, Cause: err}
}

// RunAttached runs a deliberately interactive command with the caller's
// terminal attached. It is reserved for explicitly requested setup flows
// such as ACME, where a child program needs to ask the administrator for DNS
// provider details. Normal manager commands must use Run instead.
func RunAttached(parent context.Context, command Command) error {
	if command.Path == "" {
		return fmt.Errorf("command path is required")
	}
	if command.Timeout <= 0 {
		command.Timeout = 30 * time.Second
	}
	executionContext, cancel := context.WithTimeout(parent, command.Timeout)
	defer cancel()
	execution := exec.CommandContext(executionContext, command.Path, command.Args...)
	if len(command.Env) > 0 {
		execution.Env = append(os.Environ(), command.Env...)
	}
	execution.Stdin, execution.Stdout, execution.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := execution.Run(); err != nil {
		if executionContext.Err() == context.DeadlineExceeded {
			return fmt.Errorf("timed out after %s", command.Timeout)
		}
		return err
	}
	return nil
}

func redact(value string, secrets []string) string {
	for _, secret := range secrets {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[REDACTED]")
		}
	}
	return value
}

func displayCommand(command Command) string {
	return redact(strings.Join(append([]string{command.Path}, command.Args...), " "), command.Redact)
}
