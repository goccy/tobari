package main

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

func main() {}
