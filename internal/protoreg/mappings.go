package protoreg

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// Mappings holds decorative renames merged from any number of JSON files.
// Two kinds of renames are supported:
//   - message name: obfuscated TypeUrl → friendly message name
//   - field name:   (message, field) → friendly field name
//
// Mappings are decorative only — proto resolution always uses the original
// (obfuscated) names.
//
// JSON file format (backward-compatible):
//
//	{
//	  "type.ankama.com/ij": "Hello",               // simple: just message rename
//	  "type.ankama.com/kl": {                      // extended: message + field renames
//	    "name": "Goodbye",
//	    "fields": {
//	      "abc": "userId",                         // field keyed by original name
//	      "2":   "timestamp"                       // field keyed by number
//	    }
//	  }
//	}
//
// Keys are normalized by stripping everything up to and including the last
// "/", so "type.ankama.com/ij" and "ij" both address the same message.
type Mappings struct {
	mu     sync.RWMutex
	types  map[string]string
	fields map[string]map[string]string
}

func NewMappings() *Mappings {
	return &Mappings{
		types:  map[string]string{},
		fields: map[string]map[string]string{},
	}
}

// Reload replaces the entire mapping table from the given JSON files.
// Later files override earlier ones on key collision.
func (x *Mappings) Reload(paths []string) error {
	types := map[string]string{}
	fields := map[string]map[string]string{}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		for k, v := range raw {
			key := normalizeMsgKey(k)
			// Try simple string form first.
			var s string
			if err := json.Unmarshal(v, &s); err == nil {
				types[key] = s
				continue
			}
			// Extended object form.
			var obj struct {
				Name   string            `json:"name"`
				Fields map[string]string `json:"fields"`
			}
			if err := json.Unmarshal(v, &obj); err != nil {
				return fmt.Errorf("%s: %s: %w", p, k, err)
			}
			if obj.Name != "" {
				types[key] = obj.Name
			}
			if len(obj.Fields) > 0 {
				if fields[key] == nil {
					fields[key] = map[string]string{}
				}
				for fk, fv := range obj.Fields {
					fields[key][fk] = fv
				}
			}
		}
	}
	x.mu.Lock()
	x.types = types
	x.fields = fields
	x.mu.Unlock()
	return nil
}

// Friendly returns the friendly name for a TypeUrl, falling back to the input
// if no mapping exists.
func (x *Mappings) Friendly(typeUrl string) string {
	key := normalizeMsgKey(typeUrl)
	x.mu.RLock()
	defer x.mu.RUnlock()
	if v, ok := x.types[key]; ok {
		return v
	}
	return typeUrl
}

// FieldName returns the friendly name for a field inside the given message,
// or "" if no override is set. msgRef may be a full TypeUrl or a bare
// FullName; field can be matched by original name or by number (as string).
func (x *Mappings) FieldName(msgRef string, fd protoreflect.FieldDescriptor) string {
	key := normalizeMsgKey(msgRef)
	x.mu.RLock()
	defer x.mu.RUnlock()
	fields, ok := x.fields[key]
	if !ok {
		return ""
	}
	if v, ok := fields[string(fd.Name())]; ok {
		return v
	}
	if v, ok := fields[fmt.Sprintf("%d", fd.Number())]; ok {
		return v
	}
	return ""
}

func normalizeMsgKey(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}
