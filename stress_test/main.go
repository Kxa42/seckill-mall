package main

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

func main() {
	//目标地址
	url := "http://localhost:8080/order"

	//请求体（JSON）
	body := `{"user_id": 1, "product_id": 1001, "count": 1}`

	//并发数
	concurrentNum := 20

	var wg sync.WaitGroup
	wg.Add(concurrentNum)

	fmt.Printf("开始压力测试：%d 个并发请求 -> %s\n", concurrentNum, url)
	startTime := time.Now()

	for i := 0; i < concurrentNum; i++ {
		go func(index int) {
			defer wg.Done()

			//创建HTTP请求
			req, _ := http.NewRequest("POST", url, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			//发送请求
			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				fmt.Printf("请求 %d 失败，网络错误: %v\n", index, err)
				return
			}
			defer resp.Body.Close()

			respBody, _ := io.ReadAll(resp.Body)

			if resp.StatusCode == 200 {
				fmt.Printf("[请求 %d 成功]，抢到了!(200)\n", index)
			} else if resp.StatusCode == 429 {
				fmt.Printf("[请求 %d 被限流]，活动太火爆了，请稍后再试(429):%s\n", index, string(respBody))
			} else {
				fmt.Printf("[请求 %d 失败]，状态码: %d\n", index, resp.StatusCode)
			}
		}(i)
	}
	wg.Wait()
	fmt.Printf("压力测试完成，耗时: %v\n", time.Since(startTime))
}
