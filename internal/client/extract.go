package client

import (
	"io/fs"
	"os"
	"path/filepath"
)

// ExtractExtension 把嵌入的扩展原地更新到 dst（先清空旧文件），返回目录。
// 路径不变，用户只需在扩展页点“重新加载”。
func ExtractExtension(src fs.FS, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	// 清理旧文件，避免残留（只删本目录下内容）
	ents, _ := os.ReadDir(dst)
	for _, e := range ents {
		os.RemoveAll(filepath.Join(dst, e.Name()))
	}
	return fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if filepath.Ext(p) == ".go" {
			return nil
		}
		t := filepath.Join(dst, filepath.FromSlash(p))
		if d.IsDir() {
			return os.MkdirAll(t, 0o755)
		}
		b, err := fs.ReadFile(src, p)
		if err != nil {
			return err
		}
		return os.WriteFile(t, b, 0o644)
	})
}
