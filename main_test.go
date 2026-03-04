package main

import (
	"errors"
	"testing"
)

func TestRunNoArgs(t *testing.T) {
	err := run(nil)
	if !errors.Is(err, errUsage) {
		t.Fatalf("expected errUsage, got %v", err)
	}
}

func TestRunEmptyArgs(t *testing.T) {
	err := run([]string{})
	if !errors.Is(err, errUsage) {
		t.Fatalf("expected errUsage, got %v", err)
	}
}

func TestRunBadMagnet(t *testing.T) {
	err := run([]string{"not-a-magnet"})
	if err == nil {
		t.Fatal("expected error for bad magnet")
	}
}
