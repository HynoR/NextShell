package cloudv2

import (
	"encoding/json"
	"errors"
	"fmt"
	"nextshell.dev/v2/internal/model"
	"reflect"
	"time"
)

type Record struct {
	Kind      string          `json:"kind"`
	ID        string          `json:"id"`
	Meta      model.Meta      `json:"meta"`
	Tombstone bool            `json:"tombstone,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
	Secret    *string         `json:"secret,omitempty"`
}
type Snapshot struct {
	Version     int      `json:"version"`
	WorkspaceID string   `json:"workspaceId"`
	Name        string   `json:"name"`
	Records     []Record `json:"records"`
}

func (s Snapshot) Validate() error {
	if s.Version != Version || !ValidID(s.WorkspaceID) || s.Name == "" || len(s.Name) > 1024 || len(s.Records) > 100000 {
		return errors.New("invalid snapshot")
	}
	total := 0
	seen := map[string]Record{}
	for _, r := range s.Records {
		total += len(r.Data)
		if r.Secret != nil {
			total += len(*r.Secret)
		}
		if total > MaxPlaintext {
			return errors.New("snapshot exceeds plaintext size limit")
		}
		if !ValidID(r.ID) || r.Meta.Scope != s.WorkspaceID || r.Meta.Revision < 1 {
			return errors.New("invalid resource ID/scope/revision")
		}
		created, e := time.Parse(time.RFC3339Nano, r.Meta.CreatedAt)
		if e != nil {
			return e
		}
		updated, e := time.Parse(time.RFC3339Nano, r.Meta.UpdatedAt)
		if e != nil || updated.Before(created) || r.Meta.CreatedAt != created.UTC().Format("2006-01-02T15:04:05.000Z") || r.Meta.UpdatedAt != updated.UTC().Format("2006-01-02T15:04:05.000Z") {
			return errors.New("invalid resource timestamp")
		}
		if r.Meta.DeletedAt != "" {
			deleted, e := time.Parse(time.RFC3339Nano, r.Meta.DeletedAt)
			if e != nil || deleted.Before(created) || deleted.After(updated) {
				return errors.New("invalid deletion timestamp")
			}
		}
		switch r.Kind {
		case "folder", "connection", "key", "proxy", "command", "credential":
		default:
			return errors.New("unknown resource kind")
		}
		k := r.Kind + ":" + r.ID
		if _, ok := seen[k]; ok {
			return errors.New("duplicate resource")
		}
		seen[k] = r
		if r.Tombstone {
			if r.Meta.DeletedAt == "" || len(r.Data) != 0 || r.Secret != nil {
				return errors.New("invalid tombstone")
			}
			continue
		}
		if len(r.Data) == 0 || len(r.Data) > 4<<20 {
			return errors.New("invalid record payload size")
		}
		var basic struct {
			ID string `json:"id"`
			model.Meta
		}
		if e = json.Unmarshal(r.Data, &basic); e != nil {
			return e
		}
		if basic.ID != r.ID || !reflect.DeepEqual(basic.Meta, r.Meta) {
			return errors.New("record identity/metadata mismatch")
		}
		if r.Kind != "connection" && r.Meta.DeletedAt != "" {
			return errors.New("active non-connection has deletion timestamp")
		}
		if r.Kind == "connection" || r.Kind == "key" || r.Kind == "proxy" || r.Kind == "credential" {
			if r.Secret == nil || len(*r.Secret) > 4<<20 {
				return errors.New("missing encrypted-record secret")
			}
		} else if r.Secret != nil {
			return errors.New("unexpected secret")
		}
		switch r.Kind {
		case "connection":
			var c model.Connection
			if e = strictJSON(r.Data, &c); e == nil {
				e = c.Validate()
				if e == nil && c.Deleted != (r.Meta.DeletedAt != "") {
					e = errors.New("connection trash metadata mismatch")
				}
			}
		case "folder":
			var f model.Folder
			if e = strictJSON(r.Data, &f); e == nil {
				var normalized string
				normalized, e = model.NormalizeFolderName(f.Name)
				if e == nil && normalized != f.Name {
					e = errors.New("folder name must be canonical")
				}
			}
		case "credential":
			var cr model.Credential
			if e = strictJSON(r.Data, &cr); e == nil {
				e = cr.Validate()
			}
		case "proxy":
			var p model.Proxy
			if e = strictJSON(r.Data, &p); e == nil {
				e = p.Validate()
			}
		case "key":
			var k model.Key
			e = strictJSON(r.Data, &k)
			if e == nil && k.Name == "" {
				e = errors.New("key name required")
			}
			if e == nil {
				var sec struct{ Private, Passphrase string }
				e = strictJSON([]byte(*r.Secret), &sec)
			}
		case "command":
			var c model.Command
			e = strictJSON(r.Data, &c)
			if e == nil && (c.Name == "" || c.Text == "" || len(c.Text) > 1<<20) {
				e = errors.New("command name/text required")
			}
		}
		if e != nil {
			return fmt.Errorf("%s: %w", k, e)
		}
	}
	names := map[string]bool{}
	for _, r := range s.Records {
		if r.Tombstone {
			continue
		}
		var refs struct {
			FolderID string `json:"folderId"`
			ParentID string `json:"parentId"`
			KeyID    string `json:"keyId"`
			ProxyID  string `json:"proxyId"`
			CredID   string `json:"credentialId"`
		}
		json.Unmarshal(r.Data, &refs)
		for _, ref := range []struct{ kind, id string }{{"folder", refs.FolderID}, {"folder", refs.ParentID}, {"key", refs.KeyID}, {"proxy", refs.ProxyID}, {"credential", refs.CredID}} {
			if ref.id != "" {
				target, ok := seen[ref.kind+":"+ref.id]
				if !ok || target.Tombstone {
					return errors.New("dangling or cross-workspace reference")
				}
			}
		}
		if r.Kind == "folder" {
			var f model.Folder
			json.Unmarshal(r.Data, &f)
			name := f.ParentID + "\x00" + f.Name
			if names[name] {
				return errors.New("duplicate sibling folder name")
			}
			names[name] = true
			visited := map[string]bool{r.ID: true}
			for id, depth := f.ParentID, 0; id != ""; depth++ {
				if visited[id] || depth >= 30 {
					return errors.New("folder cycle/depth")
				}
				visited[id] = true
				var parent model.Folder
				json.Unmarshal(seen["folder:"+id].Data, &parent)
				id = parent.ParentID
			}
		}
	}
	return nil
}
