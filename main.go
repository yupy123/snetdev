package main

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

// ParseIPFile 已移动到 ipfile.go

// parseCIDR 解析CIDR格式的IP段
func parseCIDR(cidr string) ([]string, error) {
	ip, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("无效的CIDR格式: %w", err)
	}

	// 仅支持IPv4
	if ip.To4() == nil {
		return nil, errors.New("仅支持IPv4地址")
	}

	// 计算CIDR包含的总IP数量（提前检测过大网段以避免OOM）
	ones, bits := ipNet.Mask.Size()
	if bits != 32 {
		return nil, errors.New("仅支持IPv4地址")
	}
	total := uint64(1) << uint64(32-ones)
	if total > 1000000 {
		return nil, errors.New("CIDR包含的IP数量过多，已终止解析")
	}

	// 预分配slice容量
	ips := make([]string, 0, int(total))

	// 从网段起始地址开始迭代（使用可变字节数组 current）
	start := ip.Mask(ipNet.Mask).To4()
	current := make([]byte, 4)
	copy(current, start)

	for ; ipNet.Contains(net.IP(current)); incIP(current) {
		ips = append(ips, net.IPv4(current[0], current[1], current[2], current[3]).String())
	}

	return ips, nil
}

// incIP 将IPv4地址的最后一个字节加1 (处理IP递增)
func incIP(ip []byte) {
	for i := len(ip) - 1; i >= 0; i-- {
		ip[i]++
		if ip[i] != 0 {
			break
		}
	}
}

// parseIPRange 解析IP段格式 (如 192.168.18.102-192.168.18.104)
func parseIPRange(rangeStr string) ([]string, error) {
	parts := strings.Split(rangeStr, "-")
	if len(parts) != 2 {
		return nil, errors.New("IP段格式错误，应为 起始IP-结束IP")
	}

	startIP := net.ParseIP(strings.TrimSpace(parts[0]))
	endIP := net.ParseIP(strings.TrimSpace(parts[1]))

	if startIP == nil || endIP == nil {
		return nil, errors.New("起始或结束IP地址无效")
	}

	// 转换为IPv4字节数组
	startBytes := startIP.To4()
	endBytes := endIP.To4()
	if startBytes == nil || endBytes == nil {
		return nil, errors.New("仅支持IPv4地址段")
	}

	// 检查起始IP是否小于等于结束IP
	if compareIP(startBytes, endBytes) > 0 {
		return nil, errors.New("起始IP不能大于结束IP")
	}

	// 计算IP段包含的数量并在过大时拒绝
	startVal := uint32(startBytes[0])<<24 | uint32(startBytes[1])<<16 | uint32(startBytes[2])<<8 | uint32(startBytes[3])
	endVal := uint32(endBytes[0])<<24 | uint32(endBytes[1])<<16 | uint32(endBytes[2])<<8 | uint32(endBytes[3])
	count := uint64(endVal - startVal + 1)
	if count > 1000000 {
		return nil, errors.New("IP段包含的IP数量过多，已终止解析")
	}

	ips := make([]string, 0, int(count))
	current := make([]byte, 4)
	copy(current, startBytes)

	// 遍历IP段中的所有IP
	for compareIP(current, endBytes) <= 0 {
		ips = append(ips, net.IPv4(current[0], current[1], current[2], current[3]).String())
		incIP(current)
	}

	return ips, nil
}

// compareIP 比较两个IPv4地址的大小 (返回 1: a>b, 0: a==b, -1: a<b)
func compareIP(a, b []byte) int {
	for i := 0; i < 4; i++ {
		if a[i] > b[i] {
			return 1
		} else if a[i] < b[i] {
			return -1
		}
	}
	return 0
}

// ping implementation moved to ping.go

// Ping functions moved to ping.go

// pingWithRetry 对单个IP执行ping探测，支持重试和超时
// 参数:
//
//	ip - 目标IP
//	maxRetries - 最大重试次数（包含第一次）
//	timeout - 单次ping超时时间
//
// 返回: bool - 是否探测成功

// 测试示例
func main() {
	// 替换为你的IP文件路径
	ips, err := ParseIPFile("iplist.txt")
	if err != nil {
		fmt.Printf("解析IP文件失败: %v\n", err)
		return
	}

	// fmt.Printf("解析出的IP总数: %d\n", len(ips))
	// for _, ip := range ips {
	// 	fmt.Println(ip)
	// }

	successIPs, failedIPs, err := PingIPs(ips, 20)
	if err != nil {
		fmt.Printf("执行ping探测失败: %v\n", err)
		return
	}

	// 输出结果
	fmt.Println("=== Ping成功的IP ===")
	for _, ip := range successIPs {
		fmt.Println(ip)
	}

	fmt.Println("\n=== Ping失败的IP ===")
	for _, ip := range failedIPs {
		fmt.Println(ip)
	}

}
