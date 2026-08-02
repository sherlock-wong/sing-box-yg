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
		fmt.Fprintln(output, "  1. 查看当前规则")
		fmt.Fprintln(output, "  2. 添加规则")
		fmt.Fprintln(output, "  3. 删除规则")
		fmt.Fprintln(output, "  0. 返回并应用")
		fmt.Fprint(output, "请选择：")
		if !input.Scan() {
			return state, false, input.Err()
		}
		switch strings.TrimSpace(input.Text()) {
		case "1":
			showRealmRules(output, candidate.Rules)
		case "2":
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
		case "3":
			if len(candidate.Rules) == 0 {
				fmt.Fprintln(output, "当前没有可删除的规则。")
				continue
			}
			showRealmRules(output, candidate.Rules)
			fmt.Fprint(output, "选择要删除的规则编号（输入 0 取消）：")
			if !input.Scan() {
				return state, false, input.Err()
			}
			selection, err := strconv.Atoi(strings.TrimSpace(input.Text()))
			if err != nil || selection < 0 || selection > len(candidate.Rules) {
				fmt.Fprintln(output, "规则编号无效。")
				continue
			}
			if selection == 0 {
				continue
			}
			index := selection - 1
			candidate.Rules = append(candidate.Rules[:index], candidate.Rules[index+1:]...)
			changed = true
		case "0":
			return candidate, changed, nil
		default:
			fmt.Fprintln(output, "无效选择。")
		}
	}
}

func showRealmRules(output io.Writer, rules []realm.Rule) {
	if len(rules) == 0 {
		fmt.Fprintln(output, "\n当前没有 Realm 转发规则。")
		return
	}
	fmt.Fprintln(output, "\n当前 Realm 转发规则：")
	for index, rule := range rules {
		fmt.Fprintf(output, "  %d. %s → %s（%s）\n", index+1, endpoint(rule.ListenHost, rule.ListenPort), endpoint(rule.RemoteHost, rule.RemotePort), rule.ID)
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
