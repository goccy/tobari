package pkga

import "example.com/crossdeps/pkgb"

func QuadGreet(name string) string {
	return pkgb.Greet(name) + "!" + pkgb.Greet(name) + "!"
}

func Quadruple(x int) int {
	return pkgb.Double(pkgb.Double(x))
}
