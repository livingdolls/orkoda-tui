package publication

import (
	"context"
	"errors"
	"testing"
)

type fakeCredentials struct{ value string }

func (f fakeCredentials) Get(context.Context, string) (string, error) { return f.value, nil }

type fakeGitPusher struct {
	workspace string
	remote    string
	branch    string
	token     string
}

func (f *fakeGitPusher) Push(_ context.Context, workspace, remote, branch, token string) error {
	f.workspace, f.remote, f.branch, f.token = workspace, remote, branch, token
	return nil
}

func TestParseGitHubRemote(t *testing.T) {
	for _, remote := range []string{
		"git@github.com:octo/example.git",
		"https://github.com/octo/example.git",
	} {
		owner, repository, pushURL, err := parseGitHubRemote(remote)
		if err != nil || owner != "octo" || repository != "example" || pushURL != "https://github.com/octo/example.git" {
			t.Fatalf("parseGitHubRemote(%q) = %q/%q/%q, %v", remote, owner, repository, pushURL, err)
		}
	}
	if _, _, _, err := parseGitHubRemote("https://evil.example/octo/example.git"); err == nil {
		t.Fatal("non-GitHub remote unexpectedly accepted")
	}
}

func TestGitHubPublisherPushesWithKeychainCredential(t *testing.T) {
	git := &fakeGitPusher{}
	publisher, err := NewGitHubPublisher(fakeCredentials{value: "secret-token"})
	if err != nil {
		t.Fatal(err)
	}
	publisher.git = git
	result, err := publisher.Publish(context.Background(), RemotePublishInput{
		RepositoryURL: "git@github.com:octo/example.git",
		WorkspacePath: "/tmp/workspace",
		Branch:        "orkoda/change-1",
		TargetBranch:  "main",
		Account:       "github",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Provider != "github" || git.branch != "orkoda/change-1" || git.token != "secret-token" {
		t.Fatalf("result/git = %#v/%#v", result, git)
	}
}

func TestGitHubPublisherRejectsUnsafeBranch(t *testing.T) {
	publisher, err := NewGitHubPublisher(fakeCredentials{value: "token"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = publisher.Publish(context.Background(), RemotePublishInput{
		RepositoryURL: "git@github.com:octo/example.git",
		WorkspacePath: "/tmp/workspace",
		Branch:        "../escape",
	})
	if err == nil || errors.Is(err, ErrRemoteUnavailable) {
		t.Fatalf("unsafe branch error = %v", err)
	}
}
