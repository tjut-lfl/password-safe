package client

import (
	"io"
	"strings"
)

// IsNativeHostInvocation：Chrome/Edge 以 "chrome-extension://<id>/" 作为参数启动原生主机。
func IsNativeHostInvocation(args []string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, "chrome-extension://") {
			return true
		}
	}
	return false
}

type nativeReq struct {
	Cmd string `json:"cmd"`
}

// RunNativeHost 处理浏览器的原生消息，转发给托盘实例。只支持 status / reconnect / guide。
func RunNativeHost(dir string, in io.Reader, out io.Writer) error {
	for {
		var req nativeReq
		if err := ReadNativeMessage(in, &req); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		resp := map[string]any{"ok": false}
		switch req.Cmd {
		case "status", "reconnect", "guide":
			r, err := CallControl(dir, req.Cmd)
			if err != nil {
				resp["error"] = err.Error()
			} else {
				resp["ok"] = true
				resp["status"] = r["status"]
			}
		default:
			resp["error"] = "unknown_cmd"
		}
		if err := WriteNativeMessage(out, resp); err != nil {
			return err
		}
	}
}
