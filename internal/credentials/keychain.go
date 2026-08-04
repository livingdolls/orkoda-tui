package credentials

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

var ErrUnavailable = errors.New("OS keychain is unavailable")
var ErrNotFound = errors.New("credential not found")

type Runner interface {
	Run(context.Context, string, []string, string) (string, error)
}

type Store struct {
	service  string
	platform string
	runner   Runner
}

func NewOSStore(service string) (*Store, error) {
	service = strings.TrimSpace(service)
	if service == "" {
		return nil, fmt.Errorf("credential service is required")
	}
	return &Store{service: service, platform: runtime.GOOS, runner: commandRunner{}}, nil
}

// NewStore allows deterministic tests to exercise the platform-specific
// command contract without invoking the real keychain.
func NewStore(service, platform string, runner Runner) (*Store, error) {
	service = strings.TrimSpace(service)
	if service == "" || strings.TrimSpace(platform) == "" || runner == nil {
		return nil, fmt.Errorf("credential service, platform, and runner are required")
	}
	return &Store{service: service, platform: platform, runner: runner}, nil
}

func (s *Store) Get(ctx context.Context, account string) (string, error) {
	if s == nil || s.runner == nil {
		return "", ErrUnavailable
	}
	account = strings.TrimSpace(account)
	if account == "" {
		return "", fmt.Errorf("credential account is required")
	}
	name, args, input, err := s.command("get", account, "")
	if err != nil {
		return "", err
	}
	value, runErr := s.runner.Run(ctx, name, args, input)
	if runErr != nil {
		if errors.Is(runErr, ErrNotFound) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("read OS keychain credential: %w", classify(runErr))
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ErrNotFound
	}
	return value, nil
}

func (s *Store) Set(ctx context.Context, account, value string) error {
	if s == nil || s.runner == nil {
		return ErrUnavailable
	}
	account = strings.TrimSpace(account)
	value = strings.TrimSpace(value)
	if account == "" || value == "" || strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("credential account and a single-line value are required")
	}
	name, args, _, err := s.command("set", account, value)
	if err != nil {
		return err
	}
	if _, runErr := s.runner.Run(ctx, name, args, value); runErr != nil {
		return fmt.Errorf("write OS keychain credential: %w", classify(runErr))
	}
	return nil
}

func (s *Store) Delete(ctx context.Context, account string) error {
	if s == nil || s.runner == nil {
		return ErrUnavailable
	}
	account = strings.TrimSpace(account)
	if account == "" {
		return fmt.Errorf("credential account is required")
	}
	name, args, input, err := s.command("delete", account, "")
	if err != nil {
		return err
	}
	if _, runErr := s.runner.Run(ctx, name, args, input); runErr != nil && !errors.Is(runErr, ErrNotFound) {
		return fmt.Errorf("delete OS keychain credential: %w", classify(runErr))
	}
	return nil
}

func (s *Store) command(operation, account, value string) (string, []string, string, error) {
	switch s.platform {
	case "darwin":
		switch operation {
		case "get":
			return "security", []string{"find-generic-password", "-a", account, "-s", s.service, "-w"}, "", nil
		case "set":
			return "security", []string{"add-generic-password", "-U", "-a", account, "-s", s.service, "-w", value}, "", nil
		case "delete":
			return "security", []string{"delete-generic-password", "-a", account, "-s", s.service}, "", nil
		}
	case "linux", "freebsd", "openbsd", "netbsd":
		switch operation {
		case "get":
			return "secret-tool", []string{"lookup", "service", s.service, "account", account}, "", nil
		case "set":
			return "secret-tool", []string{"store", "--label", s.service, "service", s.service, "account", account}, value, nil
		case "delete":
			return "secret-tool", []string{"clear", "service", s.service, "account", account}, "", nil
		}
	}
	return "", nil, "", fmt.Errorf("%w: no keychain adapter for %s", ErrUnavailable, s.platform)
}

type commandRunner struct{}

func (commandRunner) Run(ctx context.Context, name string, args []string, input string) (string, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Stdin = strings.NewReader(input)
	var output bytes.Buffer
	command.Stdout = &output
	if err := command.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", ErrUnavailable
		}
		return "", err
	}
	return output.String(), nil
}

func classify(err error) error {
	if errors.Is(err, ErrUnavailable) {
		return ErrUnavailable
	}
	if errors.Is(err, ErrNotFound) {
		return ErrNotFound
	}
	return err
}
