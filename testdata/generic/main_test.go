package main

import (
	"fmt"
	"testing"
)

func TestOk(t *testing.T) {
	r := Ok(42)
	if !r.IsOk() {
		t.Error("expected Ok")
	}
	if got := r.Unwrap(); got != 42 {
		t.Errorf("Unwrap() = %d, want 42", got)
	}
}

func TestErr(t *testing.T) {
	r := Err[int](fmt.Errorf("fail"))
	if r.IsOk() {
		t.Error("expected Err")
	}
	if got := r.UnwrapOr(99); got != 99 {
		t.Errorf("UnwrapOr() = %d, want 99", got)
	}
}

func TestUnwrapOr(t *testing.T) {
	r := Ok("hello")
	if got := r.UnwrapOr("fallback"); got != "hello" {
		t.Errorf("UnwrapOr() = %q, want %q", got, "hello")
	}
}
