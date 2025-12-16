package main

import (
	"fmt"
)

func add(a, b int) int {
	return a + b
}

func multiply(a, b int) int {
	if a == 0 || b == 0 {
		return 0
	}
	return a * b
}

func divide(a, b int) int {
	if b == 0 {
		panic("division by zero")
	}
	return a / b
}

func main() {
	result1 := add(3, 4)
	result2 := multiply(5, 6)
	result3 := divide(10, 2)
	
	fmt.Printf("Add: %d\n", result1)
	fmt.Printf("Multiply: %d\n", result2)
	fmt.Printf("Divide: %d\n", result3)
}