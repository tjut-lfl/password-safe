package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/tjut-lfl/password-safe/internal/crypto"
	"github.com/tjut-lfl/password-safe/internal/vault"
)

// unlockWithPW 解锁并把主密码交给回调（回调返回后清零）。
func unlockWithPW(fn func(v *vault.Vault, master []byte) error) error {
	v, err := openVault()
	if err != nil {
		return err
	}
	pw, err := readMasterPassword("主密码: ")
	if err != nil {
		return err
	}
	defer crypto.Zero(pw)
	if err := v.Unlock(pw); err != nil {
		return err
	}
	defer v.Lock()
	return fn(v, pw)
}

// readPassphrase 读取导出/导入口令；非交互时可用 PASSWORD_SAFE_EXPORT_PASSPHRASE。
func readPassphrase(prompt string, confirm bool) ([]byte, error) {
	if env := os.Getenv("PASSWORD_SAFE_EXPORT_PASSPHRASE"); env != "" && !term.IsTerminal(int(syscall.Stdin)) {
		return []byte(env), nil
	}
	p, err := readSecret(prompt)
	if err != nil {
		return nil, err
	}
	if confirm {
		p2, err := readSecret("再次输入确认: ")
		if err != nil {
			crypto.Zero(p)
			return nil, err
		}
		same := string(p) == string(p2)
		crypto.Zero(p2)
		if !same {
			crypto.Zero(p)
			return nil, fmt.Errorf("两次输入不一致")
		}
	}
	return p, nil
}

func writeSecretFile(path string, data []byte, force bool) error {
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if !force {
		flags |= os.O_EXCL
	}
	f, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("文件已存在: %s（加 --force 覆盖）", path)
		}
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func isPlainFormat(f string) bool {
	switch f {
	case vault.FormatCSV, vault.FormatCSVFull, vault.FormatJSON, vault.FormatBitwarden:
		return true
	}
	return false
}

func defaultExportName(format string) string {
	ts := time.Now().Format("20060102-150405")
	switch format {
	case vault.FormatJSONEncrypted:
		return "password-safe-export-" + ts + ".json"
	case vault.FormatJSON:
		return "password-safe-plain-" + ts + ".json"
	case vault.FormatBitwarden:
		return "password-safe-bitwarden-" + ts + ".json"
	case vault.FormatCSV, vault.FormatCSVFull:
		return "password-safe-" + ts + ".csv"
	}
	return "vault-export.dat"
}

func cmdExport() *cobra.Command {
	var out, format string
	var plaintext, useMaster, force bool
	c := &cobra.Command{
		Use:   "export",
		Short: "导出（encrypted | json-encrypted | csv | csv-full | json | bitwarden）",
		Long: `导出保险箱（换设备 / 备份迁移也可用这里）。
  encrypted       （默认）保险箱文件原样副本，仍用同一主密码解锁
  json-encrypted  可移植加密 JSON 包（Argon2id + AES-256-GCM），任意后续版本均可 import；
                  默认要求单独设置导出口令，--use-master 则用主密码。换设备推荐此格式。
  csv / csv-full  明文 CSV（Google/Chrome 兼容表头 name,url,username,password,note；full 另含 tags,totp_secret）
  json            明文 JSON（password-safe 自有 schema）
  bitwarden       明文 Bitwarden JSON
明文格式必须加 --plaintext，交互终端还需输入 yes 确认；文件权限 0600。
换设备：先 export --format json-encrypted，到新机器 password-safe import -f；明文 CSV 仅作短期迁移，用完 shred。`,
		RunE: func(cmd *cobra.Command, args []string) error {
			format = strings.ToLower(format)
			if out == "" {
				out = filepath.Join(".", defaultExportName(format))
			}
			switch format {
			case vault.FormatEncrypted:
				v, err := openVault()
				if err != nil {
					return err
				}
				if _, err := os.Stat(out); err == nil && !force {
					return fmt.Errorf("文件已存在: %s（加 --force 覆盖）", out)
				}
				if err := v.ExportEncrypted(out); err != nil {
					return err
				}
				fmt.Println("已导出（加密保险箱副本，用同一主密码解锁）:", out)
				return nil
			case vault.FormatJSONEncrypted:
				return unlockWithPW(func(v *vault.Vault, master []byte) error {
					entries, err := v.Entries()
					if err != nil {
						return err
					}
					pass, src := master, "master"
					if !useMaster {
						p, err := readPassphrase("设置导出口令（至少 8 位，导入时需要）: ", true)
						if err != nil {
							return err
						}
						defer crypto.Zero(p)
						pass, src = p, "passphrase"
					}
					data, err := vault.ExportBundle(entries, pass, Version, src)
					if err != nil {
						return err
					}
					if err := writeSecretFile(out, data, force); err != nil {
						return err
					}
					fmt.Printf("已导出 %d 条（加密 JSON 包，口令来源: %s）: %s\n", len(entries), map[string]string{"master": "主密码", "passphrase": "导出口令"}[src], out)
					return nil
				})
			case vault.FormatCSV, vault.FormatCSVFull, vault.FormatJSON, vault.FormatBitwarden:
				if !plaintext {
					return fmt.Errorf("%s 是明文格式，所有密码将以明文写入文件。确认请加 --plaintext", format)
				}
				fmt.Fprintln(os.Stderr, "⚠ 警告：明文导出文件包含全部密码。用完请立即 shred -u 删除，切勿上传网盘/聊天工具。")
				if term.IsTerminal(int(syscall.Stdin)) {
					ans, err := readLine("输入 yes 继续: ")
					if err != nil || strings.TrimSpace(ans) != "yes" {
						return fmt.Errorf("已取消")
					}
				}
				return unlockWithPW(func(v *vault.Vault, _ []byte) error {
					entries, err := v.Entries()
					if err != nil {
						return err
					}
					var data []byte
					switch format {
					case vault.FormatCSV:
						data, err = vault.ExportCSV(entries, false)
					case vault.FormatCSVFull:
						data, err = vault.ExportCSV(entries, true)
					case vault.FormatJSON:
						data, err = vault.ExportPlainJSON(entries, Version)
					default:
						data, err = vault.ExportBitwarden(entries)
					}
					if err != nil {
						return err
					}
					defer crypto.Zero(data)
					if err := writeSecretFile(out, data, force); err != nil {
						return err
					}
					fmt.Printf("已导出 %d 条明文（%s，权限 0600）: %s\n", len(entries), format, out)
					return nil
				})
			}
			return fmt.Errorf("未知格式 %q", format)
		},
	}
	c.Flags().StringVarP(&out, "out", "o", "", "输出路径")
	c.Flags().StringVar(&format, "format", vault.FormatEncrypted, "encrypted|json-encrypted|csv|csv-full|json|bitwarden")
	c.Flags().BoolVar(&plaintext, "plaintext", false, "确认导出明文（csv/csv-full/json/bitwarden 必需）")
	c.Flags().BoolVar(&useMaster, "use-master", false, "json-encrypted 使用主密码而非单独导出口令")
	c.Flags().BoolVar(&force, "force", false, "覆盖已存在的输出文件")
	return c
}

func cmdImport() *cobra.Command {
	var file, format string
	var skipDup bool
	c := &cobra.Command{
		Use:   "import",
		Short: "导入（自动识别 csv / json / json-encrypted / encrypted / bitwarden）",
		Long: `导入条目到当前保险箱，格式自动识别（--format 可强制指定）。
  CSV：Google/Chrome/Edge 导出、本程序 csv/csv-full、Bitwarden CSV 等常见表头
  json-encrypted：需要导出时的口令（或主密码）
  encrypted：另一个保险箱文件/备份，需要它自己的主密码
--skip-duplicates 按 网站域名+用户名 跳过已存在的条目。`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if file == "" && len(args) == 1 {
				file = args[0]
			}
			if file == "" {
				return fmt.Errorf("需要 --file")
			}
			raw, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			defer crypto.Zero(raw)
			if format == "" || format == "auto" {
				format = vault.DetectFormat(raw)
				if format == "" {
					return fmt.Errorf("无法识别导入文件格式，请用 --format 指定")
				}
			}
			return unlockWithPW(func(v *vault.Vault, master []byte) error {
				var pass []byte
				if vault.NeedsPassphrase(format) {
					prompt := "导出包口令（直接回车则使用当前主密码）: "
					if format == vault.FormatEncrypted {
						prompt = "该保险箱文件的主密码（直接回车则使用当前主密码）: "
					}
					p, err := readPassphrase(prompt, false)
					if err != nil {
						return err
					}
					if len(p) == 0 {
						p = append([]byte(nil), master...)
					}
					defer crypto.Zero(p)
					pass = p
				}
				entries, f, err := vault.ParseImport(raw, format, pass)
				if err != nil {
					return err
				}
				res, err := v.ImportEntries(entries, skipDup)
				if err != nil {
					return err
				}
				fmt.Printf("格式 %s：已导入 %d 条，跳过 %d 条\n", f, res.Added, res.Skipped)
				return nil
			})
		},
	}
	c.Flags().StringVarP(&file, "file", "f", "", "导入文件路径")
	c.Flags().StringVar(&format, "format", "auto", "auto|csv|csv-full|json|json-encrypted|encrypted|bitwarden")
	c.Flags().BoolVar(&skipDup, "skip-duplicates", false, "按 域名+用户名 去重，已存在则跳过")
	return c
}

func printBackups(v *vault.Vault) error {
	list, err := v.ListBackups()
	if err != nil {
		return err
	}
	fmt.Println("备份目录:", v.BackupDir())
	if len(list) == 0 {
		fmt.Println("(暂无备份)")
		return nil
	}
	for _, b := range list {
		fmt.Printf("%s  %-11s  %-14s  %6d B  %s\n", b.ModTime.Format("2006-01-02 15:04:05"), b.Kind, b.Version, b.Size, b.Name)
	}
	return nil
}

func cmdBackup() *cobra.Command {
	c := &cobra.Command{
		Use:   "backup",
		Short: "立即创建加密备份（backup list 查看）",
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := openVault()
			if err != nil {
				return err
			}
			p, err := v.CreateBackup(vault.BackupManual)
			if err != nil {
				return err
			}
			fmt.Println("已备份（加密态，同一主密码）:", p)
			return nil
		},
	}
	c.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "列出备份",
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := openVault()
			if err != nil {
				return err
			}
			return printBackups(v)
		},
	})
	return c
}

func cmdBackups() *cobra.Command {
	return &cobra.Command{
		Use:   "backups",
		Short: "列出备份（同 backup list）",
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := openVault()
			if err != nil {
				return err
			}
			return printBackups(v)
		},
	}
}

func cmdRestore() *cobra.Command {
	var yes bool
	c := &cobra.Command{
		Use:   "restore <备份文件>",
		Short: "从备份恢复（先校验可解密，并先备份当前保险箱）",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			src := args[0]
			v, err := openVault()
			if err != nil {
				return err
			}
			if _, err := os.Stat(src); err != nil {
				// 允许只写文件名
				alt := filepath.Join(v.BackupDir(), src)
				if _, err2 := os.Stat(alt); err2 != nil {
					return err
				}
				src = alt
			}
			pw, err := readMasterPassword("备份文件的主密码: ")
			if err != nil {
				return err
			}
			defer crypto.Zero(pw)
			n, ver, err := vault.VerifyFile(src, pw)
			if err != nil {
				return fmt.Errorf("备份校验失败，未做任何修改: %w", err)
			}
			fmt.Printf("备份可解密：%d 条，格式 v%d\n", n, ver)
			if !yes && term.IsTerminal(int(syscall.Stdin)) {
				ans, _ := readLine("将覆盖当前保险箱（会先自动备份当前版本）。输入 yes 继续: ")
				if strings.TrimSpace(ans) != "yes" {
					return fmt.Errorf("已取消")
				}
			}
			pre, err := v.Restore(src, pw)
			if err != nil {
				return err
			}
			if pre != "" {
				fmt.Println("原保险箱已备份到:", pre)
			}
			fmt.Println("已恢复:", v.Path())
			fmt.Println("若 serve 正在运行，请 systemctl --user restart password-safe 后重新解锁扩展。")
			return nil
		},
	}
	c.Flags().BoolVarP(&yes, "yes", "y", false, "跳过确认")
	return c
}
