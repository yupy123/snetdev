package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
)

// ParseIPFile 读取IP文件并解析所有IP地址
// 参数: filename - 要读取的文件路径
// 返回: []string - 解析后的所有IP地址列表, error - 处理过程中的错误
func ParseIPFile(filename string) ([]string, error) {
	// 打开文件
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("打开文件失败: %w", err)
	}
	defer file.Close()

	var ipList []string
	scanner := bufio.NewScanner(file)
	lineNum := 0

	// 逐行读取文件
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// 跳过空行和注释行
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}

		// 解析不同格式的IP
		var ips []string
		switch {
		// 处理CIDR格式 (如 192.168.18.128/30)
		case strings.Contains(line, "/"):
			ips, err = parseCIDR(line)
			if err != nil {
				return nil, fmt.Errorf("第%d行解析CIDR失败: %w", lineNum, err)
			}
		// 处理IP段格式 (如 192.168.18.102-192.168.18.104)
		case strings.Contains(line, "-"):
			ips, err = parseIPRange(line)
			if err != nil {
				return nil, fmt.Errorf("第%d行解析IP段失败: %w", lineNum, err)
			}
		// 处理单个IP格式
		default:
			if net.ParseIP(line) == nil {
				return nil, fmt.Errorf("第%d行无效的IP地址: %s", lineNum, line)
			}
			ips = []string{line}
		}

		// 将解析出的IP添加到结果列表
		ipList = append(ipList, ips...)
	}

	// 检查扫描过程中是否有错误
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("读取文件内容失败: %w", err)
	}

	return ipList, nil
}
