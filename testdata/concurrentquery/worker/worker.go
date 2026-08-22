package worker

import "example.com/concurrentquery/mathops"

// Run exercises cross-package calls so that the whole-program dependency
// analysis produces non-empty supplementary deps for this function. The
// coverage runtime then resolves those deps on every coverprofile query,
// which is the code path the concurrent test targets.
func Run(n int) int {
	nums := make([]int, n)
	for i := range nums {
		nums[i] = i + 1
	}
	scaled := mathops.Scale(nums, 3)
	total := mathops.Sum(scaled)
	return mathops.Clamp(total, 1, 1<<30)
}
