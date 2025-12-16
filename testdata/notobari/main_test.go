package main

import (
	"testing"
)

func TestAdd(t *testing.T) {
	tests := []struct {
		a, b, want int
	}{
		{1, 2, 3},
		{0, 5, 5},
		{-1, 1, 0},
		{10, -5, 5},
	}

	for _, tt := range tests {
		if got := add(tt.a, tt.b); got != tt.want {
			t.Errorf("add(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestMultiply(t *testing.T) {
	tests := []struct {
		a, b, want int
	}{
		{2, 3, 6},
		{0, 5, 0},
		{5, 0, 0},
		{-2, 3, -6},
		{-2, -3, 6},
	}

	for _, tt := range tests {
		if got := multiply(tt.a, tt.b); got != tt.want {
			t.Errorf("multiply(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestDivide(t *testing.T) {
	tests := []struct {
		a, b, want int
	}{
		{6, 2, 3},
		{10, 5, 2},
		{-6, 2, -3},
		{6, -2, -3},
	}

	for _, tt := range tests {
		if got := divide(tt.a, tt.b); got != tt.want {
			t.Errorf("divide(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestDividePanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("divide(5, 0) did not panic")
		}
	}()
	divide(5, 0)
}
