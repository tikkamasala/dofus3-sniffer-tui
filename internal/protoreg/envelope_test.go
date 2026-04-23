package protoreg

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/bufbuild/protocompile"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
	"google.golang.org/protobuf/types/known/anypb"

	"sniffer-tui/internal/capture"
)

// compileInMemory produces a FileDescriptor from an in-memory .proto source.
func compileInMemory(t *testing.T, name, src string) protoreflect.FileDescriptor {
	t.Helper()
	c := protocompile.Compiler{
		Resolver: protocompile.WithStandardImports(&protocompile.SourceResolver{
			Accessor: protocompile.SourceAccessorFromMap(map[string]string{name: src}),
		}),
	}
	files, err := c.Compile(context.Background(), name)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return files[0]
}

const testProto = `
syntax = "proto3";
package test;
import "google/protobuf/any.proto";

message Event   { google.protobuf.Any content = 1; }
message Request { google.protobuf.Any content = 1; }
message Response{ google.protobuf.Any content = 1; }

message Inner { string name = 1; int32 value = 2; }

message Message {
  oneof Content {
    Request  request  = 1;
    Response response = 2;
    Event    event    = 3;
  }
}
`

func TestEnvelopeDetection(t *testing.T) {
	fd := compileInMemory(t, "test.proto", testProto)

	byName := map[protoreflect.FullName]protoreflect.MessageDescriptor{}
	walkMessages(fd.Messages(), func(md protoreflect.MessageDescriptor) {
		byName[md.FullName()] = md
	})

	env, err := detectEnvelope(byName, "Message")
	if err != nil {
		t.Fatalf("detectEnvelope: %v", err)
	}
	if env.ContentOneof == nil || string(env.ContentOneof.Name()) != "Content" {
		t.Fatalf("oneof not found: %v", env.ContentOneof)
	}
	if len(env.AnyFieldByOption) != 3 {
		t.Fatalf("expected 3 options, got %d", len(env.AnyFieldByOption))
	}
	if string(env.AnyTypeUrlField.Name()) != "type_url" || string(env.AnyValueField.Name()) != "value" {
		t.Fatalf("Any fields: %s / %s", env.AnyTypeUrlField.Name(), env.AnyValueField.Name())
	}
}

func TestRegistryReloadAndExtract(t *testing.T) {
	// Write both proto files to a temp dir so Compile() can open them.
	dir := t.TempDir()
	write := func(name, content string) string {
		p := dir + "/" + name
		if err := writeFile(p, content); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
		return p
	}
	envelopePath := write("envelope.proto", testProto)

	reg := New("Message")
	if err := reg.Reload(context.Background(), []string{envelopePath}); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !reg.HasEnvelope() {
		t.Fatal("registry missing envelope")
	}

	// Build a real framed message: Event { Any{type_url, value=Inner-bytes} }.
	innerDesc := reg.Lookup("type.test.com/test.Inner")
	if innerDesc == nil {
		t.Fatal("Inner descriptor not registered")
	}
	inner := dynamicpb.NewMessage(innerDesc)
	inner.Set(innerDesc.Fields().ByName("name"), protoreflect.ValueOfString("hi"))
	inner.Set(innerDesc.Fields().ByName("value"), protoreflect.ValueOfInt32(7))
	innerBytes, err := proto.Marshal(inner)
	if err != nil {
		t.Fatalf("marshal inner: %v", err)
	}

	anyMsg := &anypb.Any{TypeUrl: "type.test.com/test.Inner", Value: innerBytes}

	eventDesc := reg.Lookup("type.test.com/test.Event")
	if eventDesc == nil {
		t.Fatal("Event descriptor not registered")
	}
	event := dynamicpb.NewMessage(eventDesc)
	// Copy the Any bytes into the event's dynamic 'content' field.
	anyBytes, _ := proto.Marshal(anyMsg)
	eventAnyField := eventDesc.Fields().ByName("content")
	anyDyn := dynamicpb.NewMessage(eventAnyField.Message())
	_ = proto.Unmarshal(anyBytes, anyDyn)
	event.Set(eventAnyField, protoreflect.ValueOfMessage(anyDyn))

	msgDesc := reg.Lookup("type.test.com/test.Message")
	if msgDesc == nil {
		t.Fatal("Message descriptor not registered")
	}
	envelope := dynamicpb.NewMessage(msgDesc)
	envelope.Set(msgDesc.Fields().ByName("event"), protoreflect.ValueOfMessage(event))
	payload, err := proto.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	cm := &capture.CapturedMessage{Raw: payload}
	reg.ExtractEnvelope(cm)
	if cm.Err != nil {
		t.Fatalf("ExtractEnvelope: %v", cm.Err)
	}
	if cm.TypeUrl != "type.test.com/test.Inner" {
		t.Fatalf("TypeUrl: %q", cm.TypeUrl)
	}
	if !strings.Contains(string(cm.InnerRaw), "hi") {
		t.Fatalf("InnerRaw missing payload: %q", cm.InnerRaw)
	}
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

// Nested-oneof envelope (no Any). Mirrors the Dofus connection protocol shape.
const nestedProto = `
syntax = "proto3";
message leo {
  oneof fsap {
    leq fsam = 1;
    les fsan = 2;
    leu fsao = 3;
  }
}
message leq {
  string fsat = 1;
  oneof fsbb {
    lev fsau = 2;
    lez fsav = 3;
  }
}
message les {
  string fsbf = 1;
  oneof fsbm {
    lew fsbg = 2;
  }
}
message leu {
  oneof fsbr {
    lex fsbq = 1;
  }
}
message lev {}
message lez { string fscf = 1; }
message lew {}
message lex { string hi = 1; }
`

func TestEnvelopeDetection_NestedOneof(t *testing.T) {
	fd := compileInMemory(t, "leo.proto", nestedProto)
	byName := map[protoreflect.FullName]protoreflect.MessageDescriptor{}
	walkMessages(fd.Messages(), func(md protoreflect.MessageDescriptor) {
		byName[md.FullName()] = md
	})
	env, err := detectEnvelope(byName, "leo")
	if err != nil {
		t.Fatalf("detectEnvelope: %v", err)
	}
	if env.style != styleNested {
		t.Fatalf("want styleNested, got %d", env.style)
	}
	if env.AnyFieldByOption != nil {
		t.Fatalf("Any fields should be unset for nested style")
	}
}

func TestExtract_NestedOneof_ReachesLeaf(t *testing.T) {
	dir := t.TempDir()
	p := dir + "/leo.proto"
	if err := writeFile(p, nestedProto); err != nil {
		t.Fatal(err)
	}

	reg := New("leo")
	if err := reg.Reload(context.Background(), []string{p}); err != nil {
		t.Fatalf("reload: %v", err)
	}

	// Build leo{leq{fsat="req-1", fsav: lez{fscf="payload"}}} manually.
	lezDesc := reg.Lookup("lez")
	if lezDesc == nil {
		t.Fatal("lez not registered")
	}
	lez := dynamicpb.NewMessage(lezDesc)
	lez.Set(lezDesc.Fields().ByName("fscf"), protoreflect.ValueOfString("payload"))

	leqDesc := reg.Lookup("leq")
	leq := dynamicpb.NewMessage(leqDesc)
	leq.Set(leqDesc.Fields().ByName("fsat"), protoreflect.ValueOfString("req-1"))
	leq.Set(leqDesc.Fields().ByName("fsav"), protoreflect.ValueOfMessage(lez))

	leoDesc := reg.Lookup("leo")
	leo := dynamicpb.NewMessage(leoDesc)
	leo.Set(leoDesc.Fields().ByName("fsam"), protoreflect.ValueOfMessage(leq))
	raw, err := proto.Marshal(leo)
	if err != nil {
		t.Fatal(err)
	}

	cm := &capture.CapturedMessage{Raw: raw}
	reg.ExtractEnvelope(cm)
	if cm.Err != nil {
		t.Fatalf("ExtractEnvelope: %v", cm.Err)
	}
	if cm.TypeUrl != "lez" {
		t.Fatalf("TypeUrl: got %q, want %q", cm.TypeUrl, "lez")
	}
	// InnerRaw should decode back to lez{fscf="payload"}.
	decoded := dynamicpb.NewMessage(lezDesc)
	if err := proto.Unmarshal(cm.InnerRaw, decoded); err != nil {
		t.Fatalf("unmarshal leaf: %v", err)
	}
	if got := decoded.Get(lezDesc.Fields().ByName("fscf")).String(); got != "payload" {
		t.Fatalf("leaf content: %q", got)
	}
}
