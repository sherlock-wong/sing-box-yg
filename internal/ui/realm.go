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
		if changed {
			fmt.Fprintln(output, "  ⚠ 当前显示的是未保存草稿，Realm 服务和防火墙规则尚未变更。")
		}
		fmt.Fprintln(output, "  1. 查看当前规则")
		fmt.Fprintln(output, "  2. 添加规则")
		fmt.Fprintln(output, "  3. 删除规则")
		if changed {
			fmt.Fprintln(output, "  4. 保存草稿、应用规则并返回主菜单")
			fmt.Fprintln(output, "  5. 删除草稿并返回主菜单")
		}
		fmt.Fprintln(output, "  0. 返回主菜单")
		fmt.Fprint(output, "请选择：")
		if !input.Scan() {
			return state, false, input.Err()
		}
		switch strings.TrimSpace(input.Text()) {
		case "1":
			showRealmRules(output, candidate.Rules, changed)
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
			fmt.Fprintln(output, "⚠ 规则已暂存，尚未写入 Realm 或 UFW；可选择 4 保存应用、5 删除草稿，或 0 返回时再决定。")
		case "3":
			if len(candidate.Rules) == 0 {
				fmt.Fprintln(output, "当前没有可删除的规则。")
				continue
			}
			showRealmRules(output, candidate.Rules, changed)
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
			fmt.Fprintln(output, "⚠ 删除操作已暂存，尚未写入 Realm 或 UFW；可选择 4 保存应用、5 删除草稿，或 0 返回时再决定。")
		case "4":
			if changed {
				return candidate, true, nil
			}
			fmt.Fprintln(output, "当前没有草稿需要保存。")
		case "5":
			if changed {
				fmt.Fprintln(output, "草稿已删除，已生效的 Realm 规则和防火墙保持不变。")
				return state, false, nil
			}
			fmt.Fprintln(output, "当前没有草稿需要删除。")
		case "0":
			if !changed {
				return state, false, nil
			}
			fmt.Fprint(output, "当前已经设置了草稿：1 保存并应用规则  2 删除草稿  0 继续编辑：")
			if !input.Scan() {
				return state, false, input.Err()
			}
			switch strings.TrimSpace(input.Text()) {
			case "1":
				return candidate, true, nil
			case "2":
				fmt.Fprintln(output, "草稿已删除，已生效的 Realm 规则和防火墙保持不变。")
				return state, false, nil
			case "", "0":
				continue
			default:
				fmt.Fprintln(output, "无效选择，请继续编辑或再次选择 0 返回。")
			}
		default:
			fmt.Fprintln(output, "无效选择。")
		}
	}
}

func showRealmRules(output io.Writer, rules []realm.Rule, draft bool) {
	if len(rules) == 0 {
		if draft {
			fmt.Fprintln(output, "\n草稿中没有 Realm 转发规则（尚未生效）。")
		} else {
			fmt.Fprintln(output, "\n当前没有 Realm 转发规则。")
		}
		return
	}
	if draft {
		fmt.Fprintln(output, "\nRealm 转发规则草稿（尚未生效）：")
	} else {
		fmt.Fprintln(output, "\n当前 Realm 转发规则：")
	}
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
