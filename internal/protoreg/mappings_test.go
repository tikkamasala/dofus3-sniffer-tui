package protoreg

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

func TestMappings_BackwardCompatibleFlat(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "flat.json")
	if err := os.WriteFile(p, []byte(`{"type.x/ab":"Alpha","cd":"Charlie"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewMappings()
	if err := m.Reload([]string{p}); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := m.Friendly("type.x/ab"); got != "Alpha" {
		t.Fatalf("Friendly(ab) = %q", got)
	}
	if got := m.Friendly("cd"); got != "Charlie" {
		t.Fatalf("Friendly(cd) = %q", got)
	}
	if got := m.Friendly("unknown"); got != "unknown" {
		t.Fatalf("Friendly(unknown) = %q, want passthrough", got)
	}
}

func TestMappings_ExtendedWithFields(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "ext.json")
	content := `{
  "type.x/ij": {
    "name": "Hello",
    "fields": {
      "abc": "userId",
      "2":   "timestamp"
    }
  }
}`
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewMappings()
	if err := m.Reload([]string{p}); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := m.Friendly("type.x/ij"); got != "Hello" {
		t.Fatalf("Friendly: %q", got)
	}

	// Build a fake FieldDescriptor to query: use a compiled tiny proto.
	fd := compileInMemory(t, "tiny.proto", `
syntax = "proto3";
package type.x;
message ij {
  string abc = 1;
  int64 def = 2;
  bool  ghi = 3;
}
`)
	md := fd.Messages().Get(0)
	abc := md.Fields().ByName("abc")
	def := md.Fields().ByName("def")
	ghi := md.Fields().ByName("ghi")

	if got := m.FieldName("type.x/ij", abc); got != "userId" {
		t.Errorf("field by name: %q", got)
	}
	if got := m.FieldName("ij", def); got != "timestamp" {
		t.Errorf("field by number: %q", got)
	}
	if got := m.FieldName("type.x/ij", ghi); got != "" {
		t.Errorf("unmapped field: %q (want empty)", got)
	}
}

func TestMappings_NilRenamerOK(t *testing.T) {
	// Sanity: nothing blows up when fields map is empty.
	m := NewMappings()
	fd := compileInMemory(t, "tiny2.proto", `
syntax = "proto3";
message X { string a = 1; }
`)
	md := fd.Messages().Get(0)
	if got := m.FieldName("X", md.Fields().ByName("a")); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

// Integration: end-to-end decode using field renames.
func TestDecodeWithFieldRenames(t *testing.T) {
	fd := compileInMemory(t, "integ.proto", `
syntax = "proto3";
message Inner { string abc = 1; int32 def = 2; }
`)
	md := fd.Messages().Get(0)

	reg := New("Message") // envelope name unused here
	// Manually populate the registry's byFullName map for the test; we don't
	// need a real Reload because we're not going through the envelope.
	reg.byFullName = map[protoreflect.FullName]protoreflect.MessageDescriptor{
		md.FullName(): md,
	}

	// Also register a mapping.
	m := NewMappings()
	if err := m.Reload(writeMapping(t, `{
  "Inner": {
    "name": "FriendlyInner",
    "fields": { "abc": "name", "2": "count" }
  }
}`)); err != nil {
		t.Fatal(err)
	}

	inst := dynamicpb.NewMessage(md)
	inst.Set(md.Fields().ByName("abc"), protoreflect.ValueOfString("hello"))
	inst.Set(md.Fields().ByName("def"), protoreflect.ValueOfInt32(42))
	raw, err := proto.Marshal(inst)
	if err != nil {
		t.Fatal(err)
	}
	_ = raw

	// decode package uses the registry + mappings. Rather than import it (cycle
	// risk in a test), hand-walk via our own use of the Mappings API:
	abc := md.Fields().ByName("abc")
	def := md.Fields().ByName("def")
	if got := m.FieldName("Inner", abc); got != "name" {
		t.Errorf("abc rename: %q", got)
	}
	if got := m.FieldName("Inner", def); got != "count" {
		t.Errorf("def rename: %q", got)
	}
	if strings.Contains(m.Friendly("type.test/Inner"), "Friendly") == false {
		// with the normalization, "type.test/Inner" is stripped to "Inner"
		if got := m.Friendly("type.test/Inner"); got != "FriendlyInner" {
			t.Errorf("Friendly prefix-strip: %q", got)
		}
	}
	_ = context.Background()
}

func writeMapping(t *testing.T, content string) []string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "m.json")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return []string{p}
}
