package main

import (
	"fmt"

	"example.com/autoskip/app"
)

func main() {
	fmt.Println(app.Compute(3))
	fmt.Println(app.Process([]int{1, 2, 3}))
	app.Keep(4)
	fmt.Println(app.Resume())
}
