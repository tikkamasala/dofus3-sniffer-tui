package protoreg

import (
	"errors"
	"fmt"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// envelopeStyle distinguishes the two envelope shapes we auto-detect.
//
// styleAnyWrapped: the classic game schema. Top-level `Message` has a oneof
// whose options each carry a `google.protobuf.Any`. The Any's type_url tells
// us the payload type.
//
// styleNested: the connection schema (e.g. `leo`). Top-level message has a
// oneof of sub-messages, each of which has its own oneof, and so on until we
// hit a leaf message. The leaf message's FullName is the payload "TypeUrl".
type envelopeStyle uint8

const (
	styleAnyWrapped envelopeStyle = iota
	styleNested
)

// Envelope describes the cached shape of the top-level message used to wrap
// each protobuf frame on the wire. Precomputing the field descriptors here
// means the hot path (per-frame unwrap) is pure Get(field) calls with no
// name lookups.
type Envelope struct {
	Desc         protoreflect.MessageDescriptor
	ContentOneof protoreflect.OneofDescriptor

	style            envelopeStyle
	AnyFieldByOption map[protoreflect.FieldNumber]protoreflect.FieldDescriptor
	AnyTypeUrlField  protoreflect.FieldDescriptor
	AnyValueField    protoreflect.FieldDescriptor
}

var (
	ErrEnvelopeNotFound = errors.New("envelope message not found")
	ErrNoOneof          = errors.New("envelope has no oneof")
	ErrNoAnyField       = errors.New("envelope oneof option has no Any-shaped field")
	ErrBadAnyShape      = errors.New("Any-shaped field missing type_url/value")
)

// detectEnvelope locates a message whose short name equals envName and
// classifies its envelope style. It auto-detects:
//
//   - AnyWrapped: if any oneof option carries an Any-shaped message field.
//   - Nested:    otherwise (the inner variants are themselves the payload
//     tree, and we descend through their oneofs at extract time).
func detectEnvelope(byFullName map[protoreflect.FullName]protoreflect.MessageDescriptor, envName string) (*Envelope, error) {
	var desc protoreflect.MessageDescriptor
	for _, md := range byFullName {
		if string(md.Name()) == envName {
			desc = md
			break
		}
	}
	if desc == nil {
		return nil, fmt.Errorf("%w: %q", ErrEnvelopeNotFound, envName)
	}
	if desc.Oneofs().Len() == 0 {
		return nil, ErrNoOneof
	}
	oneof := desc.Oneofs().Get(0)

	// Try AnyWrapped first: does at least one option's inner message look like
	// an Any (string type_url + bytes value)?
	if env, ok := tryAnyWrapped(desc, oneof); ok {
		return env, nil
	}

	// Fallback: Nested. Just validate that every oneof option is a message
	// kind — descent happens at extract time.
	fields := oneof.Fields()
	for i := 0; i < fields.Len(); i++ {
		f := fields.Get(i)
		if f.Kind() != protoreflect.MessageKind {
			return nil, fmt.Errorf("envelope oneof option %q is not a message; cannot route", f.Name())
		}
	}
	return &Envelope{
		Desc:         desc,
		ContentOneof: oneof,
		style:        styleNested,
	}, nil
}

func tryAnyWrapped(desc protoreflect.MessageDescriptor, oneof protoreflect.OneofDescriptor) (*Envelope, bool) {
	anyByOption := make(map[protoreflect.FieldNumber]protoreflect.FieldDescriptor)
	var anyMsgDesc protoreflect.MessageDescriptor
	fields := oneof.Fields()
	for i := 0; i < fields.Len(); i++ {
		f := fields.Get(i)
		if f.Kind() != protoreflect.MessageKind {
			continue
		}
		inner := f.Message()
		anyField := firstAnyShapedField(inner)
		if anyField == nil {
			continue
		}
		anyByOption[f.Number()] = anyField
		anyMsgDesc = anyField.Message()
	}
	if len(anyByOption) == 0 {
		return nil, false
	}

	typeUrlField := findField(anyMsgDesc, protoreflect.StringKind, "type_url", "typeUrl")
	valueField := findField(anyMsgDesc, protoreflect.BytesKind, "value")
	if typeUrlField == nil || valueField == nil {
		return nil, false
	}

	return &Envelope{
		Desc:             desc,
		ContentOneof:     oneof,
		style:            styleAnyWrapped,
		AnyFieldByOption: anyByOption,
		AnyTypeUrlField:  typeUrlField,
		AnyValueField:    valueField,
	}, true
}

// firstAnyShapedField returns the first message-kind field inside md whose
// target message has the shape of google.protobuf.Any: a string type_url
// field plus a bytes value field.
func firstAnyShapedField(md protoreflect.MessageDescriptor) protoreflect.FieldDescriptor {
	fs := md.Fields()
	for i := 0; i < fs.Len(); i++ {
		f := fs.Get(i)
		if f.Kind() != protoreflect.MessageKind || f.IsList() || f.IsMap() {
			continue
		}
		sub := f.Message()
		if findField(sub, protoreflect.StringKind, "type_url", "typeUrl") != nil &&
			findField(sub, protoreflect.BytesKind, "value") != nil {
			return f
		}
	}
	return nil
}

func findField(md protoreflect.MessageDescriptor, kind protoreflect.Kind, names ...string) protoreflect.FieldDescriptor {
	fs := md.Fields()
	nameSet := make(map[string]struct{}, len(names))
	for _, n := range names {
		nameSet[n] = struct{}{}
	}
	for i := 0; i < fs.Len(); i++ {
		f := fs.Get(i)
		if f.Kind() != kind {
			continue
		}
		if _, ok := nameSet[string(f.Name())]; ok {
			return f
		}
	}
	for i := 0; i < fs.Len(); i++ {
		f := fs.Get(i)
		if f.Kind() == kind {
			return f
		}
	}
	return nil
}
