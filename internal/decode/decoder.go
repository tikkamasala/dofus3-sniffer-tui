package decode

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
)

// Resolver finds protoreflect.MessageDescriptors by TypeUrl or FullName.
// protoreg.Registry satisfies it.
type Resolver interface {
	protoregistry.MessageTypeResolver
	protoregistry.ExtensionTypeResolver
	Lookup(typeUrl string) protoreflect.MessageDescriptor
}

// Renamer provides friendly names for messages and fields. protoreg.Mappings
// satisfies it. A nil Renamer is treated as empty (no renames applied).
type Renamer interface {
	Friendly(typeUrl string) string
	FieldName(msgRef string, fd protoreflect.FieldDescriptor) string
}

// Decode produces a pretty-printed rendering of a message whose raw bytes are
// `value` and whose type is identified by `typeUrl`. Resolver is used for
// the top-level descriptor and for any nested Any values; Renamer is used
// purely cosmetically to rename messages and fields in the output.
func Decode(r Resolver, ren Renamer, typeUrl string, value []byte) (string, error) {
	desc := r.Lookup(typeUrl)
	if desc == nil {
		return "", fmt.Errorf("unknown type_url: %s", typeUrl)
	}
	msg := dynamicpb.NewMessage(desc)
	if err := proto.Unmarshal(value, msg); err != nil {
		return "", fmt.Errorf("unmarshal %s: %w", typeUrl, err)
	}
	var b strings.Builder
	writeMessage(&b, r, ren, msg, typeUrl, 0)
	return b.String(), nil
}

// writeMessage serializes msg as pretty JSON-like text. msgRef is the TypeUrl
// or FullName under which this message should be looked up in the Renamer
// (may be empty to skip field renames for this level).
func writeMessage(b *strings.Builder, r Resolver, ren Renamer, msg protoreflect.Message, msgRef string, indent int) {
	desc := msg.Descriptor()
	if desc.FullName() == "google.protobuf.Any" {
		writeAny(b, r, ren, msg, indent)
		return
	}
	b.WriteString("{")
	first := true
	msg.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if !first {
			b.WriteString(",")
		}
		first = false
		b.WriteString("\n")
		writeIndent(b, indent+1)
		name := string(fd.Name())
		if ren != nil && msgRef != "" {
			if rn := ren.FieldName(msgRef, fd); rn != "" {
				name = rn
			}
		}
		fmt.Fprintf(b, "%q: ", name)
		writeValue(b, r, ren, v, fd, indent+1)
		return true
	})
	if !first {
		b.WriteString("\n")
		writeIndent(b, indent)
	}
	b.WriteString("}")
}

// writeAny decodes a google.protobuf.Any and inlines its payload under a
// "@type" annotation carrying the friendly name.
func writeAny(b *strings.Builder, r Resolver, ren Renamer, msg protoreflect.Message, indent int) {
	var typeUrl string
	var value []byte
	msg.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		switch fd.Name() {
		case "type_url":
			typeUrl = v.String()
		case "value":
			value = append([]byte(nil), v.Bytes()...)
		}
		return true
	})
	friendly := typeUrl
	if ren != nil {
		friendly = ren.Friendly(typeUrl)
	}
	innerDesc := r.Lookup(typeUrl)
	if innerDesc == nil {
		b.WriteString("{\n")
		writeIndent(b, indent+1)
		fmt.Fprintf(b, "%q: %q,\n", "@type", friendly)
		writeIndent(b, indent+1)
		fmt.Fprintf(b, "%q: %q\n", "_raw", base64.StdEncoding.EncodeToString(value))
		writeIndent(b, indent)
		b.WriteString("}")
		return
	}
	inner := dynamicpb.NewMessage(innerDesc)
	if err := proto.Unmarshal(value, inner); err != nil {
		b.WriteString("{\n")
		writeIndent(b, indent+1)
		fmt.Fprintf(b, "%q: %q,\n", "@type", friendly)
		writeIndent(b, indent+1)
		fmt.Fprintf(b, "%q: %q\n", "_error", err.Error())
		writeIndent(b, indent)
		b.WriteString("}")
		return
	}
	b.WriteString("{\n")
	writeIndent(b, indent+1)
	fmt.Fprintf(b, "%q: %q", "@type", friendly)
	inner.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		b.WriteString(",\n")
		writeIndent(b, indent+1)
		name := string(fd.Name())
		if ren != nil {
			if rn := ren.FieldName(typeUrl, fd); rn != "" {
				name = rn
			}
		}
		fmt.Fprintf(b, "%q: ", name)
		writeValue(b, r, ren, v, fd, indent+1)
		return true
	})
	b.WriteString("\n")
	writeIndent(b, indent)
	b.WriteString("}")
}

func writeValue(b *strings.Builder, r Resolver, ren Renamer, v protoreflect.Value, fd protoreflect.FieldDescriptor, indent int) {
	switch {
	case fd.IsList():
		lst := v.List()
		if lst.Len() == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteString("[")
		for i := 0; i < lst.Len(); i++ {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString("\n")
			writeIndent(b, indent+1)
			writeElement(b, r, ren, lst.Get(i), fd, indent+1)
		}
		b.WriteString("\n")
		writeIndent(b, indent)
		b.WriteString("]")
	case fd.IsMap():
		mp := v.Map()
		if mp.Len() == 0 {
			b.WriteString("{}")
			return
		}
		b.WriteString("{")
		first := true
		valueFD := fd.MapValue()
		mp.Range(func(mk protoreflect.MapKey, mv protoreflect.Value) bool {
			if !first {
				b.WriteString(",")
			}
			first = false
			b.WriteString("\n")
			writeIndent(b, indent+1)
			fmt.Fprintf(b, "%q: ", mk.String())
			writeElement(b, r, ren, mv, valueFD, indent+1)
			return true
		})
		b.WriteString("\n")
		writeIndent(b, indent)
		b.WriteString("}")
	default:
		writeElement(b, r, ren, v, fd, indent)
	}
}

func writeElement(b *strings.Builder, r Resolver, ren Renamer, v protoreflect.Value, fd protoreflect.FieldDescriptor, indent int) {
	switch fd.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		subDesc := fd.Message()
		subRef := string(subDesc.FullName())
		writeMessage(b, r, ren, v.Message(), subRef, indent)
	case protoreflect.StringKind:
		fmt.Fprintf(b, "%q", v.String())
	case protoreflect.BytesKind:
		fmt.Fprintf(b, "%q", base64.StdEncoding.EncodeToString(v.Bytes()))
	case protoreflect.BoolKind:
		if v.Bool() {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case protoreflect.Int32Kind, protoreflect.Int64Kind,
		protoreflect.Sint32Kind, protoreflect.Sint64Kind,
		protoreflect.Sfixed32Kind, protoreflect.Sfixed64Kind:
		b.WriteString(strconv.FormatInt(v.Int(), 10))
	case protoreflect.Uint32Kind, protoreflect.Uint64Kind,
		protoreflect.Fixed32Kind, protoreflect.Fixed64Kind:
		b.WriteString(strconv.FormatUint(v.Uint(), 10))
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		b.WriteString(strconv.FormatFloat(v.Float(), 'g', -1, 64))
	case protoreflect.EnumKind:
		n := v.Enum()
		if ev := fd.Enum().Values().ByNumber(n); ev != nil {
			fmt.Fprintf(b, "%q", ev.Name())
		} else {
			b.WriteString(strconv.FormatInt(int64(n), 10))
		}
	default:
		fmt.Fprintf(b, "%q", fmt.Sprintf("%v", v.Interface()))
	}
}

func writeIndent(b *strings.Builder, level int) {
	for i := 0; i < level; i++ {
		b.WriteString("  ")
	}
}
