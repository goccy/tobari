package main

import (
	"fmt"

	"example.com/initfunc/initlib"
)

func main() {
	fmt.Println(initlib.Transform("hello"))
	fmt.Println(initlib.Format("world"))
}
