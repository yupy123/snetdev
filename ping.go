package main

import (
	"context"
	"fmt"
	"math"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

// PingConfig 用于配置 ping 探测策略
type PingConfig struct {
	MaxAttempts         int
	SuccessNeed         int
	ConsecutiveFailStop int
	Timeout             time.Duration
	// AttemptInterval 为两次探测之间的等待时间（例如设置为 1s）
	AttemptInterval time.Duration
}

// 默认的 ping 配置
var defaultPingConfig = PingConfig{
	MaxAttempts:         5,
	SuccessNeed:         2,
	ConsecutiveFailStop: 3,
	Timeout:             2 * time.Second,
	AttemptInterval:     1 * time.Second,
}

// pingRunner 是可替换的单次探测执行函数，默认指向真实实现 pingWithRetry
var pingRunner = func(ip string, cfg PingConfig) bool {
	return pingWithRetry(ip, cfg)
}

// 保持向后兼容的简单API：默认使用 defaultPingConfig
func PingIPs(ips []string, concurrency int) ([]string, []string, error) {
	return PingIPsWithConfig(ips, concurrency, defaultPingConfig)
}

// PingIPsWithConfig 使用给定的 PingConfig 执行并发 ping 探测
func PingIPsWithConfig(ips []string, concurrency int, cfg PingConfig) ([]string, []string, error) {
	if concurrency <= 0 {
		return nil, nil, fmt.Errorf("并发数量必须大于0")
	}
	if len(ips) == 0 {
		return []string{}, []string{}, nil
	}

	// 1. 初始化并发控制信号量（带缓冲通道）
	semaphore := make(chan struct{}, concurrency)
	defer close(semaphore)

	// 2. 初始化结果通道和等待组
	resultChan := make(chan ipResult, len(ips))
	var wg sync.WaitGroup

	// 3. 遍历IP列表，启动goroutine进行探测
	for _, ip := range ips {
		wg.Add(1)
		// 占用信号量（控制并发）
		semaphore <- struct{}{}

		go func(ip string) {
			defer wg.Done()
			defer func() { <-semaphore }() // 释放信号量

			// 对当前IP执行ping探测，使用配置的参数
			success := pingRunner(ip, cfg)
			resultChan <- ipResult{ip: ip, success: success}
		}(ip)
	}

	// 4. 等待所有goroutine完成后关闭结果通道
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// 5. 收集结果
	var successIPs, failedIPs []string
	for res := range resultChan {
		if res.success {
			successIPs = append(successIPs, res.ip)
		} else {
			failedIPs = append(failedIPs, res.ip)
		}
	}

	return successIPs, failedIPs, nil
}

// ipResult 存储单个IP的探测结果
type ipResult struct {
	ip      string
	success bool
}

// evaluatePingSequence 根据一系列单次探测结果判断最终是否存活
// 规则：最多尝试 maxAttempts 次；在任意时点若成功次数达到 successNeed 则立即判定为存活并停止；
// 如出现 consecutiveFailStop 个连续失败则立即判定为失败并停止；结束后若总成功次数不少于 successNeed 则判定为存活。
func evaluatePingSequence(results []bool, maxAttempts, successNeed, consecutiveFailStop int) bool {
	successes := 0
	consecFails := 0
	attempts := 0
	for _, r := range results {
		if attempts >= maxAttempts {
			break
		}
		attempts++
		if r {
			successes++
			consecFails = 0
			if successes >= successNeed {
				return true
			}
		} else {
			consecFails++
			if consecFails >= consecutiveFailStop {
				return false
			}
		}
	}
	// 尽管没有提前退出，最终结果依赖于累计成功次数
	return successes >= successNeed
}

func pingWithRetry(ip string, cfg PingConfig) bool {
	// 使用配置中的策略
	maxRetries := cfg.MaxAttempts
	successNeed := cfg.SuccessNeed
	consecFailStop := cfg.ConsecutiveFailStop
	timeout := cfg.Timeout
	interval := cfg.AttemptInterval

	if maxRetries <= 0 {
		return false
	}
	if successNeed <= 0 {
		successNeed = 1
	}
	if consecFailStop <= 0 {
		consecFailStop = math.MaxInt32 // effectively disable
	}

	successes := 0
	consecFails := 0

	for i := 0; i < maxRetries; i++ {
		// 创建带超时的context
		ctx, cancel := context.WithTimeout(context.Background(), timeout)

		// 构建ping命令（跨系统兼容）并设置合适的超时参数
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			// Windows: ping -n 1 -w 超时(毫秒) IP
			cmd = exec.CommandContext(ctx, "ping", "-n", "1", "-w", fmt.Sprintf("%d", timeout.Milliseconds()), ip)
		} else if runtime.GOOS == "darwin" {
			// macOS: 使用 -c 1 -W <ms>（-W 为毫秒级别等待）
			cmd = exec.CommandContext(ctx, "ping", "-c", "1", "-W", fmt.Sprintf("%d", timeout.Milliseconds()), ip)
		} else {
			// Linux/Unix: 使用 -c 1 -W <秒>（对小于1秒的timeout向上取整为1秒）
			secs := int(math.Ceil(timeout.Seconds()))
			if secs < 1 {
				secs = 1
			}
			cmd = exec.CommandContext(ctx, "ping", "-c", "1", "-W", fmt.Sprintf("%d", secs), ip)
		}

		// 执行ping命令，收集输出用于更准确的判断
		out, err := cmd.CombinedOutput()
		// 及时释放context资源，不要使用 defer cancel() 在循环中累积
		cancel()

		ok := false
		if err == nil {
			ok = true
		} else if parsePingOutputSuccess(string(out)) {
			ok = true
		}

		if ok {
			successes++
			consecFails = 0
			if successes >= successNeed {
				return true
			}
		} else {
			consecFails++
			if consecFails >= consecFailStop {
				return false
			}
		}

		// 如果是最后一次重试，返回最终判定
		if i == maxRetries-1 {
			return successes >= successNeed
		}

		// 在尝试之间等待（如果配置了间隔且还会继续尝试）
		if interval > 0 {
			// 只有在不是最后一次尝试时才等待
			if i < maxRetries-1 {
				time.Sleep(interval)
			}
		}
	}

	return successes >= successNeed
}

// parsePingOutputSuccess 根据 ping 命令输出猜测是否成功收到回复（尽量兼容多语言/平台）
func parsePingOutputSuccess(output string) bool {
	low := strings.ToLower(output)

	// 优先使用带数量检验的正则，避免把 "0 received" 错误判断为成功
	reReceived := regexp.MustCompile(`(?i)(\b(\d+)\s+received\b)`) // 捕获数字
	if m := reReceived.FindStringSubmatch(low); len(m) >= 3 {
		// m[2] 是数字
		// 仅当收到数量大于0时判定为成功
		// 不使用 strconv.Atoi 是因为这里值较小且格式可控
		if m[2] != "0" {
			return true
		}
	}

	rePacketsReceived := regexp.MustCompile(`(?i)(\b(\d+)\s+packets\s+received\b)`)
	if m := rePacketsReceived.FindStringSubmatch(low); len(m) >= 3 {
		if m[2] != "0" {
			return true
		}
	}

	// 检查packet loss为0%的明确成功标识
	reLoss := regexp.MustCompile(`(?i)(\b(\d+)%\s*packet\s*loss\b)`)
	if m := reLoss.FindStringSubmatch(low); len(m) >= 3 {
		if m[2] == "0" {
			return true
		}
	}

	// 其他常见无歧义的成功标识
	indicators := []string{
		"bytes from",
		"ttl=",
		"time=",
		"已接收",
		"来自",
	}
	for _, s := range indicators {
		if strings.Contains(low, s) {
			return true
		}
	}

	return false
}
