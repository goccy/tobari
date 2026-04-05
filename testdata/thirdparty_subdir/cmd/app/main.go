package main

import (
	"fmt"

	"example.com/thirdparty_subdir/handler"
)

func main() {
	fmt.Println(handler.Handle("test"))
	fmt.Println(handler.HandleTransform("test"))
	fmt.Println(handler.Direct("test"))
}
