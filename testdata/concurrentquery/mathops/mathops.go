package mathops

func Sum(nums []int) int {
	total := 0
	for _, n := range nums {
		total += n
	}
	return total
}

func Scale(nums []int, factor int) []int {
	scaled := make([]int, len(nums))
	for i, n := range nums {
		scaled[i] = n * factor
	}
	return scaled
}

func Clamp(n, min, max int) int {
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}
