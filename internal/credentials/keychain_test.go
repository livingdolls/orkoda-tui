package credentials

import (
	"context"
	"errors"
	"testing"
)

type fakeRunner struct {
	name  string
	args  []string
	input string
	value string
	err   error
}

func (f *fakeRunner) Run(_ context.Context, name string, args []string, input string) (string, error) {
	f.name, f.args, f.input = name, append([]string(nil), args...), input
	return f.value, f.err
}

func TestKeychainLinuxRoundTripContract(t *testing.T) {
	runner := &fakeRunner{value: "token-value\n"}
	store, err := NewStore("orkoda", "linux", runner)
	if err != nil {
		t.Fatal(err)
	}
	value, err := store.Get(context.Background(), "github")
	if err != nil || value != "token-value" {
		t.Fatalf("Get() = %q, %v", value, err)
	}
	if runner.name != "secret-tool" || len(runner.args) < 4 || runner.args[0] != "lookup" {
		t.Fatalf("get command = %s %#v", runner.name, runner.args)
	}
	if err := store.Set(context.Background(), "github", "new-token"); err != nil {
		t.Fatal(err)
	}
	if runner.input != "new-token" {
		t.Fatalf("set input = %q", runner.input)
	}
	if err := store.Delete(context.Background(), "github"); err != nil {
		t.Fatal(err)
	}
}

func TestKeychainUnavailableAndNotFoundAreTyped(t *testing.T) {
	runner := &fakeRunner{err: ErrNotFound}
	store, err := NewStore("orkoda", "linux", runner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(context.Background(), "github"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() error = %v", err)
	}
	if _, err := NewStore("orkoda", "plan9", runner); err != nil {
		t.Fatal(err)
	}
}
