package ui

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/sherlock-wong/vps-net-manager/internal/realm"
)

// EditRealm changes only an in-memory candidate. The caller runs the Realm
// transaction after the user leaves the editor.
func EditRealm(ctx context.Context, input *bufio.Scanner, output io.Writer, state realm.State) (realm.State, bool, error) {
	_ = ctx
	candidate := state
	changed := false
	for {
		fmt.Fprintln(output, "\nRealm 端口转发（每条规则同时转发 TCP + UDP）")
		if len(candidate.Rules) == 0 {
			fmt.Fprintln(output, "  当前没有规则")
		} else {
			for _, rule := range candidate.Rules {
				fmt.Fprintf(output, "  %s：%s → %s\n", rule.ID, endpoint(rule.ListenHost, rule.ListenPort), endpoint(rule.RemoteHost, rule.RemotePort))
			}
		}
		fmt.Fprintln(output, "  1. 添加规则")
		fmt.Fprintln(output, "  2. 删除规则")
		fmt.Fprintln(output, "  0. 返回并应用")
		fmt.Fprint(output, "请选择：")
		if !input.Scan() {
			return state, false, input.Err()
		}
		switch strings.TrimSpace(input.Text()) {
		case "1":
			rule, err := promptRealmRule(input, output)
			if err != nil {
				fmt.Fprintln(output, "无效规则：", err)
				continue
			}
			candidate.Rules = append(candidate.Rules, rule)
			if err := candidate.Validate(); err != nil {
				candidate.Rules = candidate.Rules[:len(candidate.Rules)-1]
				fmt.Fprintln(output, "无效规则：", err)
				continue
			}
			changed = true
		case "2":
			fmt.Fprint(output, "输入规则 ID：")
			if !input.Scan() {
				return state, false, input.Err()
			}
			id := strings.TrimSpace(input.Text())
			index := -1
			for current, rule := range candidate.Rules {
				if rule.ID == id {
					index = current
					break
				}
			}
			if index < 0 {
				fmt.Fprintln(output, "未找到该规则。")
				continue
			}
			candidate.Rules = append(candidate.Rules[:index], candidate.Rules[index+1:]...)
			changed = true
		case "0":
			return candidate, changed, nil
		default:
			fmt.Fprintln(output, "无效选择。")
		}
	}
}

func promptRealmRule(input *bufio.Scanner, output io.Writer) (realm.Rule, error) {
	values := make([]string, 4)
	for index, label := range []string{"监听地址（默认 0.0.0.0）：", "监听端口：", "目标地址：", "目标端口："} {
		fmt.Fprint(output, label)
		if !input.Scan() {
			return realm.Rule{}, input.Err()
		}
		values[index] = strings.TrimSpace(input.Text())
	}
	if values[0] == "" {
		values[0] = "0.0.0.0"
	}
	listenPort, err := parseRealmPort(values[1])
	if err != nil {
		return realm.Rule{}, err
	}
	remotePort, err := parseRealmPort(values[3])
	if err != nil {
		return realm.Rule{}, err
	}
	identifier := make([]byte, 4)
	if _, err := rand.Read(identifier); err != nil {
		return realm.Rule{}, err
	}
	return realm.Rule{ID: "realm_" + hex.EncodeToString(identifier), ListenHost: values[0], ListenPort: listenPort, RemoteHost: values[2], RemotePort: remotePort}, nil
}

func parseRealmPort(value string) (uint16, error) {
	port, err := strconv.ParseUint(value, 10, 16)
	if err != nil || port == 0 {
		return 0, fmt.Errorf("端口必须是 1 到 65535")
	}
	return uint16(port), nil
}

func endpoint(host string, port uint16) string {
	if strings.Contains(host, ":") {
		return "[" + host + "]:" + strconv.Itoa(int(port))
	}
	return host + ":" + strconv.Itoa(int(port))
}
