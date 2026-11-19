package cli

import "sync"

// outcome 是一个并发任务的结果；序号由调用方按位置对应。
type outcome[T any] struct {
	value T
	err   error
}

// gather 并发执行 count 个同构任务，最多 concurrency 个同时在跑，结果按序号落位。
//
// 「按序号落位」是输出可复现的前提：同样的请求，返回顺序与位置参数顺序一致，
// 与哪个请求先完成无关。
//
// concurrency 小于等于 0 表示不设上限，用于请求数量本来就很小、彼此独立的场景；
// 会打远端或让服务端起版本管理工具的一批请求必须给上限。
func gather[T any](count int, concurrency int, task func(index int) (T, error)) []outcome[T] {
	outcomes := make([]outcome[T], count)
	if count == 0 {
		return outcomes
	}

	workers := concurrency
	if workers <= 0 || workers > count {
		workers = count
	}

	jobs := make(chan int)
	var group sync.WaitGroup
	group.Add(workers)
	for range workers {
		go func() {
			defer group.Done()
			for index := range jobs {
				outcomes[index].value, outcomes[index].err = task(index)
			}
		}()
	}

	for index := range count {
		jobs <- index
	}
	close(jobs)
	group.Wait()

	return outcomes
}
