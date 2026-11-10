package exchange

import (
	"crypto/des"
	"crypto/md5"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

type javaRandom struct{ seed uint64 }

func newJavaRandom(seed int64) *javaRandom {
	return &javaRandom{(uint64(seed) ^ 25214903917) & ((1 << 48) - 1)}
}
func (r *javaRandom) next(bits uint) uint32 {
	r.seed = (r.seed*25214903917 + 11) & ((1 << 48) - 1)
	return uint32(r.seed >> (48 - bits))
}
func (r *javaRandom) nextInt(bound int32) int32 {
	if bound&(bound-1) == 0 {
		return int32((int64(bound) * int64(r.next(31))) >> 31)
	}
	for {
		bits := int32(r.next(31))
		v := bits % bound
		if int64(bits)-int64(v)+int64(bound-1) >= 0 {
			return v
		}
	}
}
func (r *javaRandom) nextLong() int64 {
	return (int64(int32(r.next(32))) << 32) + int64(int32(r.next(32)))
}
func finalKey(head []byte, signed bool) []byte {
	if len(head) != 8 {
		return nil
	}
	value := func(i int) int64 {
		if signed {
			return int64(int8(head[i]))
		}
		return int64(head[i])
	}
	div := newJavaRandom(value(5)).nextInt(127)
	if div == 0 {
		return nil
	}
	r := newJavaRandom(3680984568597093857 / int64(div))
	for i := int64(0); i < value(0); i++ {
		r.nextLong()
	}
	r2 := newJavaRandom(r.nextLong())
	values := []int64{value(4), r2.nextLong(), value(7), value(3), r2.nextLong(), value(1), r.nextLong(), value(2)}
	b := make([]byte, 64)
	for i, n := range values {
		binary.BigEndian.PutUint64(b[i*8:], uint64(n))
	}
	sum := md5.Sum(b)
	return sum[:8]
}
func DecryptFinalShell(encoded string) (string, error) {
	b, e := base64.StdEncoding.Strict().DecodeString(encoded)
	if e != nil || len(b) <= 8 || (len(b)-8)%8 != 0 {
		return "", errors.New("FinalShell password unavailable: invalid ciphertext")
	}
	for _, signed := range []bool{false, true} {
		key := finalKey(b[:8], signed)
		if key == nil {
			continue
		}
		block, e := des.NewCipher(key)
		if e != nil {
			continue
		}
		plain := make([]byte, len(b)-8)
		for i := range len(plain) / 8 {
			block.Decrypt(plain[i*8:], b[8+i*8:16+i*8])
		}
		pad := int(plain[len(plain)-1])
		if pad < 1 || pad > 8 || pad > len(plain) {
			continue
		}
		ok := true
		for _, v := range plain[len(plain)-pad:] {
			if int(v) != pad {
				ok = false
			}
		}
		plain = plain[:len(plain)-pad]
		if ok && len(plain) > 0 && utf8.Valid(plain) {
			return string(plain), nil
		}
	}
	return "", errors.New("FinalShell password unavailable: cannot decrypt")
}
func DecodeFinalShell(raw []byte, group string) ([]Entry, error) {
	var rows []json.RawMessage
	var obj json.RawMessage
	if e := json.Unmarshal(raw, &obj); e != nil {
		return nil, e
	}
	if len(obj) > 0 && obj[0] == '[' {
		if e := json.Unmarshal(raw, &rows); e != nil {
			return nil, e
		}
	} else {
		rows = []json.RawMessage{obj}
	}
	var out []Entry
	for _, row := range rows {
		var v struct {
			Name, Host string
			Port       int
			User       string `json:"user_name"`
			Auth       *int   `json:"authentication_type"`
			Password   string
			KeyID      string `json:"secret_key_id"`
			Encoding   string `json:"terminal_encoding"`
			Backspace  int    `json:"backspace_key_sequence"`
		}
		if e := json.Unmarshal(row, &v); e != nil {
			return nil, e
		}
		if v.Auth == nil || strings.TrimSpace(v.User) == "" || strings.TrimSpace(v.Host) == "" {
			return nil, errors.New("invalid FinalShell connection")
		}
		if v.Port == 0 {
			v.Port = 22
		}
		if v.Port < 1 || v.Port > 65535 {
			return nil, errors.New("invalid FinalShell port")
		}
		if strings.TrimSpace(v.Name) == "" {
			v.Name = fmt.Sprintf("%s:%d", v.Host, v.Port)
		}
		e := Entry{Name: v.Name, Host: v.Host, Port: v.Port, Username: v.User, AuthType: "password", GroupPath: group, Encoding: "utf-8", Tags: []string{}, Backspace: "ascii-backspace", Delete: "vt220-delete"}
		if e.GroupPath == "" {
			e.GroupPath = "/import/finalshell"
		}
		enc := strings.NewReplacer("-", "", "_", "", " ", "").Replace(strings.ToLower(v.Encoding))
		switch enc {
		case "gb18030", "gbk", "big5":
			e.Encoding = enc
		}
		if v.Backspace == 2 {
			e.Backspace = "ascii-delete"
		}
		if strings.TrimSpace(v.KeyID) != "" {
			e.AuthType = "interactive"
			e.NeedsKeyRebind = true
		}
		if pass, err := DecryptFinalShell(v.Password); err == nil {
			e.Password = &pass
		} else {
			e.PasswordUnavailable = true
		}
		out = append(out, e)
	}
	return out, nil
}
