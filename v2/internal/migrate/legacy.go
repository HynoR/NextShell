// Package migrate reads 1.x profiles read-only, re-encrypting into an atomic v2 import.
package migrate

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"nextshell.dev/v2/internal/exchange"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/security"
	"nextshell.dev/v2/internal/sshclient"
	"nextshell.dev/v2/internal/storage"
	"path/filepath"
	"strconv"
	"strings"
)

type LegacyKeyReader func(context.Context) (string, error)
type Result struct {
	Connections, Keys, Proxies, Folders, Commands int
	Warnings                                      []string
	Already                                       bool
	Marker                                        string
}
type row map[string]any

func text(r row, k string) string {
	v := r[k]
	if v == nil {
		return ""
	}
	switch v := v.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return fmt.Sprint(v)
	}
}
func number(r row, k string, def int) int {
	v := text(r, k)
	if v == "" {
		return def
	}
	n, e := strconv.Atoi(v)
	if e != nil {
		return def
	}
	return n
}
func rows(tx *sql.Tx, table string) ([]row, error) {
	var exists int
	if e := tx.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&exists); e != nil {
		return nil, e
	}
	if exists == 0 {
		return nil, nil
	}
	q, e := tx.Query("SELECT * FROM " + table + " ORDER BY rowid")
	if e != nil {
		return nil, e
	}
	defer q.Close()
	cols, e := q.Columns()
	if e != nil {
		return nil, e
	}
	var out []row
	for q.Next() {
		vals := make([]any, len(cols))
		ptr := make([]any, len(cols))
		for i := range vals {
			ptr[i] = &vals[i]
		}
		if e = q.Scan(ptr...); e != nil {
			return nil, e
		}
		r := row{}
		for i, k := range cols {
			r[k] = vals[i]
		}
		out = append(out, r)
	}
	return out, q.Err()
}
func Run(ctx context.Context, source string, target *storage.Store, fallback LegacyKeyReader) (Result, error) {
	var result Result
	abs, e := filepath.Abs(source)
	if e != nil {
		return result, e
	}
	u := url.URL{Scheme: "file", Path: abs}
	db, e := sql.Open("sqlite", u.String()+"?mode=ro")
	if e != nil {
		return result, e
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, e = db.ExecContext(ctx, "PRAGMA query_only=ON"); e != nil {
		return result, e
	}
	tx, e := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return result, e
	}
	defer tx.Rollback()
	tables := map[string][]row{}
	for _, name := range []string{"connections", "app_settings", "secret_store", "ssh_keys", "proxies", "connection_folders", "saved_commands", "command_history", "command_template_params", "recycle_bin_entries", "schema_migrations"} {
		v, e := rows(tx, name)
		if e != nil {
			return result, e
		}
		tables[name] = v
	}
	var columns int
	if e = tx.QueryRow("SELECT count(*) FROM pragma_table_info('connections') WHERE name IN ('username','auth_type','group_path')").Scan(&columns); e != nil || columns != 3 {
		return result, errors.New("not a recognized 1.x connection database")
	}
	var version int
	for _, r := range tables["schema_migrations"] {
		if v := number(r, "version", 0); v > version {
			version = v
		}
	}
	if version > 29 {
		return result, fmt.Errorf("unknown future 1.x schema %d", version)
	}
	archive, e := json.Marshal(tables)
	if e != nil {
		return result, e
	}
	digest := sha256.Sum256(archive)
	result.Marker = hex.EncodeToString(digest[:])
	if yes, e := target.Imported(result.Marker); e != nil {
		return result, e
	} else if yes {
		result.Already = true
		return result, nil
	}
	var device string
	for _, r := range tables["app_settings"] {
		if text(r, "key") == "device_key" {
			v := text(r, "value_json")
			if v == "" {
				v = text(r, "value")
			}
			if e = json.Unmarshal([]byte(v), &device); e != nil {
				device = v
			}
		}
	}
	if device == "" && len(tables["secret_store"]) > 0 {
		if fallback == nil {
			return result, errors.New("legacy device_key missing; keytar NextShell/device-key fallback unavailable")
		}
		device, e = fallback(ctx)
		if e != nil {
			return result, fmt.Errorf("legacy keychain fallback: %w", e)
		}
	}
	key, e := hex.DecodeString(device)
	if len(tables["secret_store"]) > 0 && (e != nil || len(key) != 32) {
		return result, errors.New("invalid/missing legacy device_key; source is untouched")
	}
	secrets := map[string]row{}
	for _, r := range tables["secret_store"] {
		secrets[text(r, "id")] = r
	}
	readSecret := func(ref string) (string, error) {
		if ref == "" {
			return "", nil
		}
		if !strings.HasPrefix(ref, "secret://") {
			return "", errors.New("unsupported legacy credential reference")
		}
		r, ok := secrets[strings.TrimPrefix(ref, "secret://")]
		if !ok {
			return "", fmt.Errorf("missing legacy secret %s", strings.TrimPrefix(ref, "secret://"))
		}
		decode := func(k string) ([]byte, error) { return base64.StdEncoding.DecodeString(text(r, k)) }
		ct, e := decode("ciphertext_b64")
		if e != nil {
			return "", e
		}
		iv, e := decode("iv_b64")
		if e != nil {
			return "", e
		}
		tag, e := decode("tag_b64")
		if e != nil {
			return "", e
		}
		p, e := security.Open(ct, tag, key, iv, text(r, "aad"))
		if e != nil {
			return "", errors.New("legacy credential decryption failed; nothing imported")
		}
		return string(p), nil
	}
	b := storage.Bundle{Archive: archive}
	root := model.Folder{ID: "v1-root:" + result.Marker[:16], Name: "1.x 导入 " + result.Marker[:8]}
	b.Folders = append(b.Folders, root)
	folderIDs := map[string]string{}
	paths := map[string]string{}
	materialize := func(segments []string) string {
		parent := root.ID
		for _, name := range segments {
			if strings.TrimSpace(name) == "" {
				continue
			}
			k := parent + "\x00" + name
			if id := paths[k]; id != "" {
				parent = id
				continue
			}
			id := storage.ID()
			b.Folders = append(b.Folders, model.Folder{ID: id, Name: name, ParentID: parent})
			paths[k] = id
			parent = id
		}
		return parent
	}
	pending := append([]row{}, tables["connection_folders"]...)
	for len(pending) > 0 {
		progress := false
		var rest []row
		for _, r := range pending {
			scope := text(r, "scope_key")
			if scope != "" && scope != "local-default" {
				result.Warnings = append(result.Warnings, "cloud folder archived, not imported: "+text(r, "id"))
				continue
			}
			pid := text(r, "parent_id")
			parent := root.ID
			if pid != "" {
				parent = folderIDs[pid]
				if parent == "" {
					rest = append(rest, r)
					continue
				}
			}
			name := text(r, "name")
			k := parent + "\x00" + name
			id := paths[k]
			if id == "" {
				id = "v1-folder:" + text(r, "id")
				paths[k] = id
				b.Folders = append(b.Folders, model.Folder{ID: id, Name: name, ParentID: parent})
			}
			folderIDs[text(r, "id")] = id
			progress = true
		}
		if len(rest) > 0 && !progress {
			return result, errors.New("legacy folder cycle or missing parent")
		}
		pending = rest
	}
	local := func(r row) bool { scope := text(r, "origin_scope_key"); return scope == "" || scope == "local-default" }
	keyIDs, proxyIDs := map[string]string{}, map[string]string{}
	for _, r := range tables["ssh_keys"] {
		if !local(r) {
			result.Warnings = append(result.Warnings, "cloud key archived, not imported: "+text(r, "id"))
			continue
		}
		private, e := readSecret(text(r, "key_content_ref"))
		if e != nil {
			return result, e
		}
		pass, e := readSecret(text(r, "passphrase_ref"))
		if e != nil {
			return result, e
		}
		pub, e := sshclient.InspectKey(private, pass)
		if e != nil {
			return result, fmt.Errorf("invalid legacy key %s: %w", text(r, "name"), e)
		}
		id := "v1-key:" + text(r, "id")
		b.Keys = append(b.Keys, model.Key{ID: id, Name: text(r, "name"), Private: private, Passphrase: pass, Public: pub})
		keyIDs[text(r, "id")] = id
	}
	for _, r := range tables["proxies"] {
		if !local(r) {
			result.Warnings = append(result.Warnings, "cloud proxy archived, not imported: "+text(r, "id"))
			continue
		}
		pass, e := readSecret(text(r, "credential_ref"))
		if e != nil {
			return result, e
		}
		id := "v1-proxy:" + text(r, "id")
		b.Proxies = append(b.Proxies, model.Proxy{ID: id, Name: text(r, "name"), Kind: text(r, "proxy_type"), Host: text(r, "host"), Port: number(r, "port", 1080), User: text(r, "username"), Password: pass})
		proxyIDs[text(r, "id")] = id
	}
	for _, r := range tables["connections"] {
		if !local(r) {
			result.Warnings = append(result.Warnings, "cloud connection archived, not imported: "+text(r, "id"))
			continue
		}
		pass, e := readSecret(text(r, "credential_ref"))
		if e != nil {
			return result, e
		}
		auth := text(r, "auth_type")
		switch auth {
		case "privateKey":
			auth = "key"
		case "interactive":
			auth = "keyboard"
		}
		c := model.Connection{ID: "v1-connection:" + text(r, "id"), Name: text(r, "name"), Host: text(r, "host"), Port: number(r, "port", 22), User: text(r, "username"), Auth: auth, Secret: pass, Encoding: text(r, "terminal_encoding"), Fingerprint: text(r, "host_fingerprint"), KeyID: keyIDs[text(r, "ssh_key_id")], ProxyID: proxyIDs[text(r, "proxy_id")], Notes: text(r, "notes"), Favorite: number(r, "favorite", 0) != 0, Backspace: text(r, "backspace_mode"), DeleteMode: text(r, "delete_mode"), Monitor: number(r, "monitor_session", 0) != 0}
		if text(r, "proxy_id") != "" && c.ProxyID == "" {
			return result, errors.New("unresolved legacy proxy; refusing an unintended direct connection")
		}
		json.Unmarshal([]byte(text(r, "tags")), &c.Tags)
		if c.Encoding == "" {
			c.Encoding = "utf-8"
		}
		if number(r, "keepalive_enabled", 0) != 0 {
			c.Keepalive = number(r, "keepalive_interval_sec", 30)
		}
		c.FolderID = folderIDs[text(r, "folder_id")]
		if c.FolderID == "" {
			c.FolderID = materialize(exchange.Segments(text(r, "group_path")))
		}
		if c.Auth == "key" && c.KeyID == "" {
			if ref := text(r, "private_key_ref"); ref != "" {
				private, e := readSecret(ref)
				if e != nil {
					return result, e
				}
				pub, e := sshclient.InspectKey(private, "")
				if e != nil {
					return result, e
				}
				id := "v1-inline-key:" + text(r, "id")
				b.Keys = append(b.Keys, model.Key{ID: id, Name: c.Name + " key", Private: private, Public: pub})
				c.KeyID = id
			} else {
				c.Auth = "keyboard"
				c.NeedsKeyRebind = true
				c.OriginalAuth = "key"
				result.Warnings = append(result.Warnings, "private key path not read; rebind key: "+c.Name)
			}
		}
		if c.ProxyID == "" && text(r, "proxy_type") != "" && text(r, "proxy_type") != "none" {
			pass, e := readSecret(text(r, "proxy_credential_ref"))
			if e != nil {
				return result, e
			}
			id := "v1-inline-proxy:" + text(r, "id")
			b.Proxies = append(b.Proxies, model.Proxy{ID: id, Name: c.Name + " proxy", Kind: text(r, "proxy_type"), Host: text(r, "proxy_host"), Port: number(r, "proxy_port", 1080), User: text(r, "proxy_username"), Password: pass})
			c.ProxyID = id
		}
		b.Connections = append(b.Connections, c)
	}
	for _, r := range tables["saved_commands"] {
		b.Commands = append(b.Commands, model.Command{ID: "v1-command:" + text(r, "id"), Name: text(r, "name"), Description: text(r, "description"), Group: text(r, "group_name"), Text: text(r, "command"), Template: number(r, "is_template", 0) != 0})
	}
	for _, r := range tables["command_history"] {
		b.History = append(b.History, text(r, "command"))
	}
	if len(tables["recycle_bin_entries"]) > 0 {
		result.Warnings = append(result.Warnings, "recycle-bin entries retained in encrypted migration archive; not activated")
	}
	result.Warnings = append(result.Warnings, "preferences, template parameters and original snapshot retained in encrypted migration archive; cloud resources are not activated before phase 3")
	if e = target.ApplyBundle(b, result.Marker); e != nil {
		return result, e
	}
	result.Connections = len(b.Connections)
	result.Keys = len(b.Keys)
	result.Proxies = len(b.Proxies)
	result.Folders = len(b.Folders)
	result.Commands = len(b.Commands)
	return result, nil
}
