package model

import (
	"fmt"
	"net/url"
	"strings"
)

// Only public web URL schemes; no command/file/custom application handlers.
func ValidateTerminalURL(raw string) error {
	if len(raw) > 16384 || strings.ContainsAny(raw, "\x00\r\n") {
		return fmt.Errorf("终端链接无效")
	}
	u, e := url.Parse(raw)
	if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return fmt.Errorf("仅允许无内嵌凭据的http/https链接，不打开远端文件或自定义协议")
	}
	return nil
}

// A server-supplied directory entry must never turn an automatic download into path traversal.
func ValidateDownloadName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00\r\n") {
		return fmt.Errorf("远端名称不是安全的本地单段文件名；请使用手工下载路径")
	}
	return nil
}
