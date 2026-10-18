// Package cli 实现 password-safe 命令行。
package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/tjut-lfl/password-safe/internal/clipboard"
	"github.com/tjut-lfl/password-safe/internal/crypto"
	"github.com/tjut-lfl/password-safe/internal/server"
	"github.com/tjut-lfl/password-safe/internal/vault"

	"github.com/pquerna/otp/totp"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	vaultPath   string
	listenAddr  string
	keepBackups int
)

// Version 通过 -ldflags "-X github.com/tjut-lfl/password-safe/internal/cli.Version=..." 注入。
var Version = "dev"

// Execute 入口。
func Execute() {
	root := &cobra.Command{
		Use:          "password-safe",
		Version:      Version,
		Short:        "本地密码保险箱（CLI + localhost HTTP API）",
		SilenceUsage: true,
		Long:         "Argon2id KEK + AES-256-GCM DEK 保护账号密码的本地保险箱。支持 TOTP、剪贴板、交互 shell、搜索与树形列表。",
	}
	root.PersistentFlags().StringVar(&vaultPath, "vault", "", "保险箱文件路径（默认 ~/.password-safe/vault.dat）")
	root.PersistentFlags().IntVar(&keepBackups, "keep-backups", 0, "每类自动备份保留份数（默认 10，或环境变量 PASSWORD_SAFE_BACKUP_KEEP）")

	root.AddCommand(
		cmdInit(),
		cmdUnlock(),
		cmdLock(),
		cmdAdd(),
		cmdList(),
		cmdSearch(),
		cmdTree(),
		cmdGet(),
		cmdUpdate(),
		cmdDelete(),
		cmdGenerate(),
		cmdExport(),
		cmdImport(),
		cmdPasswd(),
		cmdMigrate(),
		cmdVerify(),
		cmdTOTP(),
		cmdShell(),
		cmdServe(),
		cmdStatus(),
		cmdBackup(),
		cmdBackups(),
		cmdRestore(),
	)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
}

func resolvePath() (string, error) {
	if vaultPath != "" {
		return vaultPath, nil
	}
	if env := os.Getenv("PASSWORD_SAFE_VAULT"); env != "" {
		return env, nil
	}
	return vault.DefaultPath()
}

func openVault() (*vault.Vault, error) {
	p, err := resolvePath()
	if err != nil {
		return nil, err
	}
	v := vault.New(p)
	v.AppVersion = Version
	v.KeepBackups = keepBackups
	if v.KeepBackups <= 0 {
		if n, err := strconv.Atoi(os.Getenv("PASSWORD_SAFE_BACKUP_KEEP")); err == nil && n > 0 {
			v.KeepBackups = n
		}
	}
	if os.Getenv("PASSWORD_SAFE_NO_AUTO_BACKUP") == "1" {
		v.DisableAutoBackup = true
	}
	if bp, err := v.EnsureVersionBackup(); err != nil {
		fmt.Fprintln(os.Stderr, "警告: 新版本首次运行备份失败:", err)
	} else if bp != "" {
		fmt.Fprintln(os.Stderr, "检测到新版本首次运行，已自动备份保险箱:", bp)
	}
	return v, nil
}

var stdinReader = bufio.NewReader(os.Stdin)

func readSecret(prompt string) ([]byte, error) {
	fmt.Fprint(os.Stderr, prompt)
	if term.IsTerminal(int(syscall.Stdin)) {
		pw, err := term.ReadPassword(int(syscall.Stdin))
		fmt.Fprintln(os.Stderr)
		return pw, err
	}
	line, err := stdinReader.ReadString('\n')
	if err != nil && len(line) == 0 {
		return nil, err
	}
	return []byte(strings.TrimRight(line, "\r\n")), nil
}

func readMasterPassword(prompt string) ([]byte, error) {
	if env := os.Getenv("PASSWORD_SAFE_MASTER"); env != "" && !term.IsTerminal(int(syscall.Stdin)) {
		return []byte(env), nil
	}
	return readSecret(prompt)
}

func readLine(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	line, err := stdinReader.ReadString('\n')
	if err != nil && len(line) == 0 {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func cmdInit() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "创建保险箱并设置主密码（v2 KEK/DEK）",
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := openVault()
			if err != nil {
				return err
			}
			if v.Exists() {
				return vault.ErrAlreadyInit
			}
			pw, err := readMasterPassword("设置主密码: ")
			if err != nil {
				return err
			}
			pw2, err := readMasterPassword("确认主密码: ")
			if err != nil {
				crypto.Zero(pw)
				return err
			}
			if string(pw) != string(pw2) {
				crypto.Zero(pw)
				crypto.Zero(pw2)
				return fmt.Errorf("两次输入的主密码不一致")
			}
			crypto.Zero(pw2)
			if len(pw) < 8 {
				crypto.Zero(pw)
				return fmt.Errorf("主密码至少 8 个字符")
			}
			if err := v.Init(pw); err != nil {
				crypto.Zero(pw)
				return err
			}
			crypto.Zero(pw)
			v.Lock()
			fmt.Println("保险箱已创建 (格式 v2 KEK/DEK):", v.Path())
			return nil
		},
	}
}

func cmdUnlock() *cobra.Command {
	return &cobra.Command{
		Use:   "unlock",
		Short: "验证主密码（CLI 一次性校验；长期会话请用 serve 或 shell）",
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := openVault()
			if err != nil {
				return err
			}
			pw, err := readMasterPassword("主密码: ")
			if err != nil {
				return err
			}
			err = v.Unlock(pw)
			crypto.Zero(pw)
			if err != nil {
				return err
			}
			fmt.Printf("解锁成功（格式 v%d）。长期会话请用 `password-safe shell` 或 `serve`。\n", v.FormatVersion())
			if v.FormatVersion() < vault.FormatVersion {
				fmt.Println("提示: 当前为旧格式，建议运行 `password-safe migrate` 升级到 v2。")
			}
			v.Lock()
			return nil
		},
	}
}

func cmdLock() *cobra.Command {
	return &cobra.Command{
		Use:   "lock",
		Short: "提示：CLI 无常驻会话；对运行中的 serve 请调用 HTTP /api/v1/lock",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("CLI 模式无常驻内存会话。交互 shell 内输入 lock/exit；serve 请：")
			fmt.Println(`  curl -X POST -H "Authorization: Bearer <token>" http://127.0.0.1:8787/api/v1/lock`)
			return nil
		},
	}
}

func withUnlocked(fn func(*vault.Vault) error) error {
	v, err := openVault()
	if err != nil {
		return err
	}
	pw, err := readMasterPassword("主密码: ")
	if err != nil {
		return err
	}
	err = v.Unlock(pw)
	crypto.Zero(pw)
	if err != nil {
		return err
	}
	defer v.Lock()
	return fn(v)
}

func parseTags(tags string) []string {
	var tagList []string
	if tags == "" {
		return tagList
	}
	for _, t := range strings.Split(tags, ",") {
		t = strings.TrimSpace(t)
		if t != "" {
			tagList = append(tagList, t)
		}
	}
	return tagList
}

func cmdAdd() *cobra.Command {
	var name, username, password, url, notes, tags, totpSecret string
	c := &cobra.Command{
		Use:   "add",
		Short: "添加条目（名称支持层级，如 work/github）",
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" {
				return fmt.Errorf("需要 --name")
			}
			if password == "" {
				pw, err := readSecret("条目密码: ")
				if err != nil {
					return err
				}
				password = string(pw)
				crypto.Zero(pw)
			}
			return withUnlocked(func(v *vault.Vault) error {
				e, err := v.Add(name, username, password, url, notes, parseTags(tags), totpSecret)
				if err != nil {
					return err
				}
				fmt.Printf("已添加: %s (id=%s)\n", e.Name, e.ID)
				return nil
			})
		},
	}
	c.Flags().StringVar(&name, "name", "", "条目标题（必填，可用 work/github 层级）")
	c.Flags().StringVar(&username, "username", "", "用户名")
	c.Flags().StringVar(&password, "password", "", "密码（省略则交互输入）")
	c.Flags().StringVar(&url, "url", "", "网址")
	c.Flags().StringVar(&notes, "notes", "", "备注")
	c.Flags().StringVar(&tags, "tags", "", "标签，逗号分隔")
	c.Flags().StringVar(&totpSecret, "totp", "", "TOTP base32 密钥（可选）")
	return c
}

func printMetaList(list []vault.EntryMeta) {
	if len(list) == 0 {
		fmt.Println("(空)")
		return
	}
	for _, m := range list {
		tags := strings.Join(m.Tags, ",")
		totpMark := ""
		if m.HasTOTP {
			totpMark = " [totp]"
		}
		id := m.ID
		if len(id) > 8 {
			id = id[:8]
		}
		fmt.Printf("%s  %s  user=%s  url=%s  tags=%s%s\n", id, m.Name, m.Username, m.URL, tags, totpMark)
	}
}

func cmdList() *cobra.Command {
	var query string
	c := &cobra.Command{
		Use:   "list",
		Short: "列出条目（不含密码）；可用 --query 过滤",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withUnlocked(func(v *vault.Vault) error {
				var list []vault.EntryMeta
				var err error
				if query != "" {
					list, err = v.Search(query)
				} else {
					list, err = v.List()
				}
				if err != nil {
					return err
				}
				printMetaList(list)
				return nil
			})
		},
	}
	c.Flags().StringVarP(&query, "query", "q", "", "按名称/用户名/URL/标签子串过滤")
	return c
}

func cmdSearch() *cobra.Command {
	return &cobra.Command{
		Use:   "search <query>",
		Short: "搜索条目（名称/用户名/URL/备注/标签）",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withUnlocked(func(v *vault.Vault) error {
				list, err := v.Search(args[0])
				if err != nil {
					return err
				}
				printMetaList(list)
				return nil
			})
		},
	}
}

func cmdTree() *cobra.Command {
	return &cobra.Command{
		Use:   "tree",
		Short: "按层级名称树形列出（如 work/github）",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withUnlocked(func(v *vault.Vault) error {
				lines, err := v.TreeLines()
				if err != nil {
					return err
				}
				for _, l := range lines {
					fmt.Println(l)
				}
				return nil
			})
		},
	}
}

func cmdGet() *cobra.Command {
	var asJSON, copyPass bool
	var clearAfter int
	c := &cobra.Command{
		Use:   "get <id|name>",
		Short: "获取并解密一条目；-c/--copy 复制密码到剪贴板（不回显密码）",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withUnlocked(func(v *vault.Vault) error {
				e, err := v.Get(args[0])
				if err != nil {
					return err
				}
				if copyPass {
					d := time.Duration(clearAfter) * time.Second
					if err := clipboard.CopyThenClear(e.Password, d); err != nil {
						return fmt.Errorf("%w\n%s", err, clipboard.Hint())
					}
					fmt.Printf("已复制密码到剪贴板（约 %ds 后自动清空）: %s\n", clearAfter, e.Name)
					fmt.Printf("Username: %s\n", e.Username)
					if e.URL != "" {
						fmt.Printf("URL:      %s\n", e.URL)
					}
					return nil
				}
				if asJSON {
					enc := json.NewEncoder(os.Stdout)
					enc.SetIndent("", "  ")
					return enc.Encode(e)
				}
				fmt.Printf("ID:       %s\n", e.ID)
				fmt.Printf("Name:     %s\n", e.Name)
				fmt.Printf("Username: %s\n", e.Username)
				fmt.Printf("Password: %s\n", e.Password)
				fmt.Printf("URL:      %s\n", e.URL)
				fmt.Printf("Notes:    %s\n", e.Notes)
				fmt.Printf("Tags:     %s\n", strings.Join(e.Tags, ", "))
				if e.TOTPSecret != "" {
					fmt.Printf("TOTP:     (已配置，使用 `totp %s` 获取验证码)\n", e.Name)
				}
				return nil
			})
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "以 JSON 输出")
	c.Flags().BoolVarP(&copyPass, "copy", "c", false, "复制密码到剪贴板并自动清空（不打印密码）")
	c.Flags().IntVar(&clearAfter, "clear-after", 30, "剪贴板自动清空秒数")
	return c
}

func cmdUpdate() *cobra.Command {
	var name, username, password, url, notes, tags, totpSecret string
	var setPass bool
	c := &cobra.Command{
		Use:   "update <id|name>",
		Short: "更新条目",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withUnlocked(func(v *vault.Vault) error {
				opts := vault.UpdateOpts{}
				if cmd.Flags().Changed("name") {
					opts.Name = &name
				}
				if cmd.Flags().Changed("username") {
					opts.Username = &username
				}
				if cmd.Flags().Changed("password") || setPass {
					if password == "" && setPass {
						pw, err := readSecret("新密码: ")
						if err != nil {
							return err
						}
						password = string(pw)
						crypto.Zero(pw)
					}
					opts.Password = &password
				}
				if cmd.Flags().Changed("url") {
					opts.URL = &url
				}
				if cmd.Flags().Changed("notes") {
					opts.Notes = &notes
				}
				if cmd.Flags().Changed("tags") {
					tl := parseTags(tags)
					opts.Tags = &tl
				}
				if cmd.Flags().Changed("totp") {
					opts.TOTPSecret = &totpSecret
				}
				e, err := v.Update(args[0], opts)
				if err != nil {
					return err
				}
				fmt.Printf("已更新: %s\n", e.Name)
				return nil
			})
		},
	}
	c.Flags().StringVar(&name, "name", "", "新名称")
	c.Flags().StringVar(&username, "username", "", "新用户名")
	c.Flags().StringVar(&password, "password", "", "新密码")
	c.Flags().BoolVar(&setPass, "ask-password", false, "交互输入新密码")
	c.Flags().StringVar(&url, "url", "", "新网址")
	c.Flags().StringVar(&notes, "notes", "", "新备注")
	c.Flags().StringVar(&tags, "tags", "", "新标签（逗号分隔）")
	c.Flags().StringVar(&totpSecret, "totp", "", "新 TOTP 密钥（空字符串可清除）")
	return c
}

func cmdDelete() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id|name>",
		Short: "删除条目",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withUnlocked(func(v *vault.Vault) error {
				if err := v.Delete(args[0]); err != nil {
					return err
				}
				fmt.Println("已删除")
				return nil
			})
		},
	}
}

func cmdGenerate() *cobra.Command {
	var length, words int
	var noSymbols, memorable bool
	var sep string
	c := &cobra.Command{
		Use:   "generate",
		Short: "生成强随机密码或可记忆口令短语",
		RunE: func(cmd *cobra.Command, args []string) error {
			if memorable {
				pp, err := crypto.GeneratePassphrase(words, sep)
				if err != nil {
					return err
				}
				fmt.Println(pp)
				return nil
			}
			pw, err := crypto.GeneratePassword(length, !noSymbols)
			if err != nil {
				return err
			}
			fmt.Println(pw)
			return nil
		},
	}
	c.Flags().IntVarP(&length, "length", "l", 20, "随机密码长度")
	c.Flags().BoolVar(&noSymbols, "no-symbols", false, "不含符号")
	c.Flags().BoolVar(&memorable, "memorable", false, "生成 diceware 风格口令短语")
	c.Flags().IntVar(&words, "words", 5, "口令短语词数（配合 --memorable）")
	c.Flags().StringVar(&sep, "separator", "-", "口令短语分隔符")
	return c
}

func cmdPasswd() *cobra.Command {
	return &cobra.Command{
		Use:     "passwd",
		Aliases: []string{"rekey"},
		Short:   "更换主密码（仅重包装 DEK，不重加密条目；需 v2）",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withUnlocked(func(v *vault.Vault) error {
				if v.FormatVersion() < 2 {
					return fmt.Errorf("请先运行 migrate 升级到 v2")
				}
				// 新密码必须走 readSecret，避免非 TTY 下误用 PASSWORD_SAFE_MASTER
				npw, err := readSecret("新主密码: ")
				if err != nil {
					return err
				}
				npw2, err := readSecret("确认新主密码: ")
				if err != nil {
					crypto.Zero(npw)
					return err
				}
				if string(npw) != string(npw2) {
					crypto.Zero(npw)
					crypto.Zero(npw2)
					return fmt.Errorf("两次输入不一致")
				}
				crypto.Zero(npw2)
				if len(npw) < 8 {
					crypto.Zero(npw)
					return fmt.Errorf("主密码至少 8 个字符")
				}
				err = v.ChangeMasterPassword(npw)
				crypto.Zero(npw)
				if err != nil {
					return err
				}
				fmt.Println("主密码已更换（DEK 已重新包装）")
				return nil
			})
		},
	}
}

func cmdMigrate() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "将 v1 保险箱升级为 v2（KEK/DEK）",
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := openVault()
			if err != nil {
				return err
			}
			pw, err := readMasterPassword("主密码: ")
			if err != nil {
				return err
			}
			err = v.Unlock(pw)
			if err != nil {
				crypto.Zero(pw)
				return err
			}
			defer v.Lock()
			if v.FormatVersion() >= vault.FormatVersion {
				crypto.Zero(pw)
				fmt.Printf("已是 v%d，无需迁移\n", v.FormatVersion())
				return nil
			}
			err = v.MigrateToV2(pw)
			crypto.Zero(pw)
			if err != nil {
				return err
			}
			fmt.Println("已迁移到 v2（KEK/DEK）；迁移前的原文件已备份到", v.BackupDir())
			return nil
		},
	}
}

func cmdVerify() *cobra.Command {
	return &cobra.Command{
		Use:   "verify",
		Short: "校验主密码与保险箱 GCM 完整性",
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := openVault()
			if err != nil {
				return err
			}
			pw, err := readMasterPassword("主密码: ")
			if err != nil {
				return err
			}
			err = v.Verify(pw)
			crypto.Zero(pw)
			if err != nil {
				return err
			}
			fmt.Println("校验通过：可解密且 GCM 标签完整")
			return nil
		},
	}
}

func cmdTOTP() *cobra.Command {
	var copyCode bool
	c := &cobra.Command{
		Use:   "totp <id|name>",
		Short: "输出条目当前 TOTP 6 位验证码",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withUnlocked(func(v *vault.Vault) error {
				e, err := v.Get(args[0])
				if err != nil {
					return err
				}
				if e.TOTPSecret == "" {
					return fmt.Errorf("该条目未配置 TOTP 密钥")
				}
				code, err := totp.GenerateCode(e.TOTPSecret, time.Now())
				if err != nil {
					return fmt.Errorf("生成 TOTP 失败（检查 base32 密钥）: %w", err)
				}
				if copyCode {
					if err := clipboard.CopyThenClear(code, 30*time.Second); err != nil {
						return err
					}
					fmt.Printf("已复制 TOTP 到剪贴板: %s\n", e.Name)
					return nil
				}
				fmt.Println(code)
				return nil
			})
		},
	}
	c.Flags().BoolVarP(&copyCode, "copy", "c", false, "复制到剪贴板")
	return c
}

func cmdStatus() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "查看保险箱是否已初始化及格式版本",
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := openVault()
			if err != nil {
				return err
			}
			fmt.Printf("path: %s\n", v.Path())
			fmt.Printf("initialized: %v\n", v.Exists())
			if v.Exists() {
				fmt.Printf("format_version: %d\n", v.FormatVersion())
			}
			return nil
		},
	}
}

func cmdServe() *cobra.Command {
	var unlockOnStart bool
	var ttlStr string
	c := &cobra.Command{
		Use:   "serve",
		Short: "启动 localhost HTTP API（默认 127.0.0.1:8787）",
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := openVault()
			if err != nil {
				return err
			}
			if !v.Exists() {
				return vault.ErrNotInitialized
			}
			if unlockOnStart {
				pw, err := readMasterPassword("主密码: ")
				if err != nil {
					return err
				}
				err = v.Unlock(pw)
				crypto.Zero(pw)
				if err != nil {
					return err
				}
				fmt.Fprintln(os.Stderr, "保险箱已在内存中解锁；通过 HTTP unlock 获取会话 token。")
			}
			addr := listenAddr
			if addr == "" {
				addr = os.Getenv("PASSWORD_SAFE_ADDR")
			}
			if addr == "" {
				addr = "127.0.0.1:8787"
			}
			srv := server.New(v, addr)
			if ttlStr != "" {
				d, err := time.ParseDuration(ttlStr)
				if err != nil || d <= 0 {
					return fmt.Errorf("无效 --ttl / PASSWORD_SAFE_SESSION_TTL: %q", ttlStr)
				}
				srv.SetTTL(d)
			}
			fmt.Fprintf(os.Stderr, "password-safe 监听 %s （仅 localhost，会话 %s）\n", srv.Addr(), server.FormatTTL(srv.TTL()))
			return srv.ListenAndServe()
		},
	}
	c.Flags().StringVar(&listenAddr, "addr", "127.0.0.1:8787", "监听地址（必须是 loopback）")
	c.Flags().BoolVar(&unlockOnStart, "unlock", false, "启动时交互输入主密码并保持解锁")
	defaultTTL := os.Getenv("PASSWORD_SAFE_SESSION_TTL")
	c.Flags().StringVar(&ttlStr, "ttl", defaultTTL, "会话有效期（Go duration，如 6h；默认 6h 或环境变量 PASSWORD_SAFE_SESSION_TTL）")
	return c
}

func cmdShell() *cobra.Command {
	return &cobra.Command{
		Use:   "shell",
		Short: "交互式 REPL：解锁一次后可执行 add/list/get/delete/search/tree/totp 等",
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := openVault()
			if err != nil {
				return err
			}
			if !v.Exists() {
				return vault.ErrNotInitialized
			}
			pw, err := readMasterPassword("主密码: ")
			if err != nil {
				return err
			}
			err = v.Unlock(pw)
			crypto.Zero(pw)
			if err != nil {
				return err
			}
			defer v.Lock()
			fmt.Println("已解锁。命令: list | search <q> | tree | get <name> [-c] | add | delete <name> | totp <name> | generate | verify | lock | exit | help")
			for {
				fmt.Fprint(os.Stderr, "safe> ")
				line, err := stdinReader.ReadString('\n')
				if err != nil {
					fmt.Fprintln(os.Stderr)
					return nil
				}
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				parts := splitShellArgs(line)
				if len(parts) == 0 {
					continue
				}
				switch parts[0] {
				case "exit", "quit", "lock":
					fmt.Println("已锁定，再见。")
					return nil
				case "help", "?":
					fmt.Println("list | search <q> | tree | get <name> [--copy] | add <name> | delete <name> | totp <name> | generate | status | exit")
				case "list":
					q := ""
					if len(parts) > 1 {
						q = parts[1]
					}
					var list []vault.EntryMeta
					if q != "" {
						list, err = v.Search(q)
					} else {
						list, err = v.List()
					}
					if err != nil {
						fmt.Println("错误:", err)
						continue
					}
					printMetaList(list)
				case "search":
					if len(parts) < 2 {
						fmt.Println("用法: search <query>")
						continue
					}
					list, err := v.Search(strings.Join(parts[1:], " "))
					if err != nil {
						fmt.Println("错误:", err)
						continue
					}
					printMetaList(list)
				case "tree":
					lines, err := v.TreeLines()
					if err != nil {
						fmt.Println("错误:", err)
						continue
					}
					for _, l := range lines {
						fmt.Println(l)
					}
				case "get":
					if len(parts) < 2 {
						fmt.Println("用法: get <name> [--copy|-c]")
						continue
					}
					name := parts[1]
					doCopy := false
					for _, p := range parts[2:] {
						if p == "-c" || p == "--copy" {
							doCopy = true
						}
					}
					e, err := v.Get(name)
					if err != nil {
						fmt.Println("错误:", err)
						continue
					}
					if doCopy {
						if err := clipboard.CopyThenClear(e.Password, 30*time.Second); err != nil {
							fmt.Println("错误:", err)
							continue
						}
						fmt.Printf("已复制密码（30s 后清空）: %s  user=%s\n", e.Name, e.Username)
					} else {
						fmt.Printf("Name: %s\nUsername: %s\nPassword: %s\nURL: %s\n", e.Name, e.Username, e.Password, e.URL)
					}
				case "add":
					if len(parts) < 2 {
						fmt.Println("用法: add <name>")
						continue
					}
					name := parts[1]
					user, _ := readLine("username: ")
					passBytes, err := readSecret("password: ")
					if err != nil {
						fmt.Println("错误:", err)
						continue
					}
					pass := string(passBytes)
					crypto.Zero(passBytes)
					url, _ := readLine("url: ")
					totpSec, _ := readLine("totp_secret (可选): ")
					e, err := v.Add(name, user, pass, url, "", nil, totpSec)
					if err != nil {
						fmt.Println("错误:", err)
						continue
					}
					fmt.Printf("已添加 %s (%s)\n", e.Name, e.ID[:8])
				case "delete", "rm":
					if len(parts) < 2 {
						fmt.Println("用法: delete <name>")
						continue
					}
					if err := v.Delete(parts[1]); err != nil {
						fmt.Println("错误:", err)
						continue
					}
					fmt.Println("已删除")
				case "totp":
					if len(parts) < 2 {
						fmt.Println("用法: totp <name>")
						continue
					}
					e, err := v.Get(parts[1])
					if err != nil {
						fmt.Println("错误:", err)
						continue
					}
					if e.TOTPSecret == "" {
						fmt.Println("未配置 TOTP")
						continue
					}
					code, err := totp.GenerateCode(e.TOTPSecret, time.Now())
					if err != nil {
						fmt.Println("错误:", err)
						continue
					}
					fmt.Println(code)
				case "generate":
					pw, err := crypto.GeneratePassword(20, true)
					if err != nil {
						fmt.Println("错误:", err)
						continue
					}
					fmt.Println(pw)
				case "status":
					fmt.Printf("entries=%d format=v%d\n", v.EntryCount(), v.FormatVersion())
				default:
					fmt.Println("未知命令，输入 help 查看")
				}
			}
		},
	}
}

func splitShellArgs(line string) []string {
	var parts []string
	var cur strings.Builder
	inQuote := false
	for _, r := range line {
		switch {
		case r == '"':
			inQuote = !inQuote
		case r == ' ' && !inQuote:
			if cur.Len() > 0 {
				parts = append(parts, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		parts = append(parts, cur.String())
	}
	return parts
}
