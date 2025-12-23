package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// 配置
const (
	BaseURL     = "http://127.0.0.1:8080"
	Concurrency = 20 // 并发数
	UserID      = 9527
	ProductID   = 1
)

type LoginResponse struct {
	Code  int    `json:"code"`
	Token string `json:"token"`
	Msg   string `json:"message"`
}

func main() {
	// 1. 先登录获取 Token
	fmt.Println("正在登录获取 Token...")
	token, err := login(UserID)
	if err != nil {
		fmt.Printf("❌ 登录失败: %v\n", err)
		return
	}
	fmt.Printf("✅ 登录成功，Token长度: %d\n", len(token))

	// 2. 开始并发压测
	fmt.Printf("🚀 开始并发压测：模拟 %d 个请求 (使用同一 Token)...\n", Concurrency)
	var wg sync.WaitGroup
	wg.Add(Concurrency)

	startTime := time.Now()

	for i := 0; i < Concurrency; i++ {
		go func(idx int) {
			defer wg.Done()
			createOrder(idx, token)
		}(i)
	}

	wg.Wait()
	fmt.Printf("\n⏱️ 压测完成，总耗时: %v\n", time.Since(startTime))
}

// 登录动作
func login(uid int) (string, error) {
	reqBody := map[string]interface{}{"user_id": uid}
	jsonData, _ := json.Marshal(reqBody)

	resp, err := http.Post(BaseURL+"/login", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var res LoginResponse
	if err := json.Unmarshal(body, &res); err != nil {
		return "", fmt.Errorf("解析响应失败: %v", err)
	}

	if res.Code != 200 {
		return "", fmt.Errorf("服务端返回错误: %s", string(body))
	}
	return res.Token, nil
}

// 下单动作
func createOrder(idx int, token string) {
	// 构造请求体 (注意：现在不需要传 user_id 了)
	reqBody := map[string]interface{}{
		"product_id": ProductID,
		"count":      1,
	}
	jsonData, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", BaseURL+"/order", bytes.NewBuffer(jsonData))

	// 🔑 关键点：设置 Authorization Header
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)

	if err != nil {
		fmt.Printf("[请求 %d] 网络错误: %v\n", idx, err)
		return
	}
	defer resp.Body.Close()

	// 读取结果
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == 200 {
		fmt.Printf("[请求 %d] ✅ 成功: %s\n", idx, string(body))
	} else if resp.StatusCode == 429 {
		fmt.Printf("[请求 %d] 🔥 限流 (429): %s\n", idx, string(body))
	} else {
		fmt.Printf("[请求 %d] ❌ 失败 (%d): %s\n", idx, resp.StatusCode, string(body))
	}
}
