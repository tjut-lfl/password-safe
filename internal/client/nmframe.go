package client

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
)

// 原生消息帧：4 字节小端长度 + UTF-8 JSON。Chrome 限制主机→浏览器 1MB。
const maxNativeMsg = 1 << 20

func ReadNativeMessage(r io.Reader, v any) error {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return err
	}
	n := binary.LittleEndian.Uint32(hdr[:])
	if n > maxNativeMsg {
		return errors.New("native message too large")
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return err
	}
	return json.Unmarshal(buf, v)
}

func WriteNativeMessage(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(b) > maxNativeMsg {
		return errors.New("native message too large")
	}
	var hdr [4]byte
	binary.LittleEndian.PutUint32(hdr[:], uint32(len(b)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}
