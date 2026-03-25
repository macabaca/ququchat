package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

const (
	TestDuration = 10 * time.Second       // 测试持续时间
	WorkerCount  = 5                      // 并发 Worker 数量
	SlowTaskTime = 2 * time.Second        // 模拟长耗时任务 (LLM)
	FastTaskTime = 100 * time.Millisecond // 模拟普通任务
)

func main() {
	fmt.Printf("开始测试... 持续时间: %v\n", TestDuration)
	fmt.Println("--------------------------------------------------")

	// 场景 1: 共享 Channel (Prefetch=4)
	runSharedTest()

	fmt.Println("--------------------------------------------------")

	// 场景 2: 独立 Channel (每个 Worker 拥有独立 Prefetch=1)
	runIndividualTest()
}

func runSharedTest() {
	var totalTasks int64
	var wg sync.WaitGroup

	// 模拟 RabbitMQ 的 Prefetch 计数器 (共享池)
	// 只有池子里有令牌时，Worker 才能领到任务
	prefetchPool := make(chan struct{}, WorkerCount)
	for i := 0; i < WorkerCount; i++ {
		prefetchPool <- struct{}{}
	}

	start := time.Now()
	for i := 0; i < WorkerCount; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for time.Since(start) < TestDuration {
				// 1. 尝试获取配额 (等同于从 RabbitMQ 接收消息)
				<-prefetchPool

				// 2. 处理任务
				if id == 0 {
					// 假设 Worker 0 运气不好，拿到了一个长任务
					time.Sleep(SlowTaskTime)
				} else {
					time.Sleep(FastTaskTime)
				}

				atomic.AddInt64(&totalTasks, 1)

				// 3. 归还配额 (等同于发送 Ack)
				prefetchPool <- struct{}{}
			}
		}(i)
	}

	wg.Wait()
	fmt.Printf("[共享模式结果] 总处理任务数: %d\n", totalTasks)
	fmt.Println("说明: 当 Worker 0 处理长任务时，它占用了共享池 1/4 的配额。")
}

func runIndividualTest() {
	var totalTasks int64
	var wg sync.WaitGroup

	start := time.Now()
	for i := 0; i < WorkerCount; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// 模拟独立 Channel: 每个 Worker 都有自己专属的配额池，容量为 1
			individualPool := make(chan struct{}, 1)
			individualPool <- struct{}{}

			for time.Since(start) < TestDuration {
				// 1. 获取自己的配额
				<-individualPool

				// 2. 处理任务
				if id == 0 {
					time.Sleep(SlowTaskTime)
				} else {
					time.Sleep(FastTaskTime)
				}

				atomic.AddInt64(&totalTasks, 1)

				// 3. 归还自己的配额
				individualPool <- struct{}{}
			}
		}(i)
	}

	wg.Wait()
	fmt.Printf("[独立模式结果] 总处理任务数: %d\n", totalTasks)
	fmt.Println("说明: 每个 Worker 独立进货。Worker 0 慢不会导致其它 Worker 拿不到配额。")
}
