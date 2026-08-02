package ui

import (
	"fmt"
	"net"
)

// promptShareAddress assigns the address embedded in this protocol's client
// links. A UDP dial selects the host's preferred IPv4 route without sending
// application traffic; interface scanning is only a fallback.
func promptShareAddress(prompt *prompt, protocolName string) (string, bool, error) {
	fallback := preferredIPv4()
	if fallback == "" {
		value, err := prompt.ask(protocolName + " 分享链接对外地址（域名、IPv4 或 IPv6；输入 0 暂不设置）：")
		if err != nil {
			return "", false, err
		}
		if value == "0" || value == "" {
			return "", false, nil
		}
		return value, true, nil
	}
	value, err := prompt.ask(fmt.Sprintf("%s 分享链接对外地址（域名、IPv4 或 IPv6；回车使用 %s；输入 0 暂不设置）：", protocolName, fallback))
	if err != nil {
		return "", false, err
	}
	if value == "0" {
		return "", false, nil
	}
	if value == "" {
		return fallback, true, nil
	}
	return value, true, nil
}

func editShareAddress(prompt *prompt, protocolName, current string) (string, bool, error) {
	if current == "" {
		return promptShareAddress(prompt, protocolName)
	}
	value, err := prompt.ask(fmt.Sprintf("%s 分享链接对外地址（当前 %s；回车保持；输入 0 取消）：", protocolName, current))
	if err != nil {
		return "", false, err
	}
	if value == "" || value == "0" {
		return current, false, nil
	}
	return value, true, nil
}

func preferredIPv4() string {
	connection, err := net.Dial("udp4", "1.1.1.1:53")
	if err == nil {
		defer connection.Close()
		if address, ok := connection.LocalAddr().(*net.UDPAddr); ok && address.IP.To4() != nil && !address.IP.IsLoopback() {
			return address.IP.String()
		}
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, item := range interfaces {
		if item.Flags&net.FlagUp == 0 || item.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, err := item.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			var ip net.IP
			switch value := address.(type) {
			case *net.IPNet:
				ip = value.IP
			case *net.IPAddr:
				ip = value.IP
			}
			if ip != nil && ip.To4() != nil && !ip.IsLoopback() {
				return ip.String()
			}
		}
	}
	return ""
}
