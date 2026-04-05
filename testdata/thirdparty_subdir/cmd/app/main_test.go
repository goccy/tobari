package main

import (
	"testing"

	"example.com/thirdparty_subdir/handler"
)

func TestDirect(t *testing.T) {
	result := handler.Direct("hello")
	if result != "saved:hello" {
		t.Fatal("unexpected:", result)
	}
}

func TestCallback(t *testing.T) {
	result := handler.Handle("")
	if result != "" {
		t.Fatal("unexpected:", result)
	}
}

func TestInterface(t *testing.T) {
	result := handler.HandleTransform("")
	if result != "" {
		t.Fatal("unexpected:", result)
	}
}
