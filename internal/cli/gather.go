package cli

import "sync"

// outcome 是一个并发任务的结果；序号由调用方按位置对应。
type outcome[T any] struct {
	value T
	err   error
}

// gather 并发执行 count 个同构任务，结果按序号落位。
//
// 「按序号落位」是输出可复现的前提：同样的请求，返回顺序与位置参数顺序一致，
// 与哪个请求先完成无关。
func gather[T any](count int, task func(index int) (T, error)) []outcome[T] {
	outcomes := make([]outcome[T], count)

	var group sync.WaitGroup
	group.Add(count)
	for index := range count {
		go func(index int) {
			defer group.Done()
			outcomes[index].value, outcomes[index].err = task(index)
		}(index)
	}
	group.Wait()
	return outcomes
}
