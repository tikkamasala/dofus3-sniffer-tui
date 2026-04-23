package protoreg

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"

	"sniffer-tui/internal/capture"
)

// Registry owns the set of protobuf descriptors loaded from disk plus the
// cached envelope shape. Reload() atomically replaces the internal state
// under a single mutex so readers always see a consistent snapshot.
type Registry struct {
	mu         sync.RWMutex
	byFullName map[protoreflect.FullName]protoreflect.MessageDescriptor
	env        *Envelope
	version    uint64
	envName    string
}

func New(envelopeMessageName string) *Registry {
	if envelopeMessageName == "" {
		envelopeMessageName = "Message"
	}
	return &Registry{
		byFullName: map[protoreflect.FullName]protoreflect.MessageDescriptor{},
		envName:    envelopeMessageName,
	}
}

// Reload replaces the registry with descriptors parsed from the given files.
// On error the previous state is preserved.
func (r *Registry) Reload(ctx context.Context, paths []string) error {
	files, err := Compile(ctx, paths)
	if err != nil {
		return err
	}
	byName := map[protoreflect.FullName]protoreflect.MessageDescriptor{}
	for _, fd := range files {
		walkMessages(fd.Messages(), func(md protoreflect.MessageDescriptor) {
			byName[md.FullName()] = md
		})
	}
	env, envErr := detectEnvelope(byName, r.envName)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.byFullName = byName
	r.env = env
	r.version++
	if envErr != nil {
		return fmt.Errorf("descriptors loaded but envelope invalid: %w", envErr)
	}
	return nil
}

func (r *Registry) Version() uint64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.version
}

// SetEnvelopeName updates the name the registry looks for when detecting its
// envelope. Takes effect on the next Reload().
func (r *Registry) SetEnvelopeName(name string) {
	if name == "" {
		name = "Message"
	}
	r.mu.Lock()
	r.envName = name
	r.mu.Unlock()
}

func (r *Registry) HasEnvelope() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.env != nil
}

// Lookup returns the descriptor for a protobuf Any.TypeUrl. The fully-qualified
// name is the suffix after the last '/'; Any.TypeUrl has the form
// "type.example.com/some.pkg.Name".
func (r *Registry) Lookup(typeUrl string) protoreflect.MessageDescriptor {
	name := typeUrl
	if i := strings.LastIndex(typeUrl, "/"); i >= 0 {
		name = typeUrl[i+1:]
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byFullName[protoreflect.FullName(name)]
}

// FindMessageByName implements protoregistry.MessageTypeResolver so
// protojson.Marshal can resolve nested Any values during pretty-printing.
func (r *Registry) FindMessageByName(name protoreflect.FullName) (protoreflect.MessageType, error) {
	r.mu.RLock()
	md, ok := r.byFullName[name]
	r.mu.RUnlock()
	if !ok {
		return nil, protoregistry.NotFound
	}
	return dynamicpb.NewMessageType(md), nil
}

// FindMessageByURL implements the TypeUrl-based lookup variant.
func (r *Registry) FindMessageByURL(url string) (protoreflect.MessageType, error) {
	md := r.Lookup(url)
	if md == nil {
		return nil, protoregistry.NotFound
	}
	return dynamicpb.NewMessageType(md), nil
}

// FindExtensionByName / FindExtensionByNumber — stubs for the resolver
// interface; the Dofus protocol doesn't use extensions.
func (r *Registry) FindExtensionByName(protoreflect.FullName) (protoreflect.ExtensionType, error) {
	return nil, protoregistry.NotFound
}

func (r *Registry) FindExtensionByNumber(protoreflect.FullName, protoreflect.FieldNumber) (protoreflect.ExtensionType, error) {
	return nil, protoregistry.NotFound
}

// ExtractEnvelope performs the cheap per-frame unwrap. It fills cm.TypeUrl and
// cm.InnerRaw (the bytes of the inner payload) or cm.Err on failure.
//
// Two envelope styles are supported and auto-detected at Reload time:
//   - AnyWrapped: the inner payload is Any.value and cm.TypeUrl is Any.type_url.
//   - Nested:    we descend through successive oneofs until reaching a leaf
//     message. cm.TypeUrl is the leaf's fully-qualified name and cm.InnerRaw
//     is the re-marshaled leaf bytes.
func (r *Registry) ExtractEnvelope(cm *capture.CapturedMessage) {
	r.mu.RLock()
	env := r.env
	r.mu.RUnlock()
	if env == nil {
		cm.Err = ErrEnvelopeNotFound
		return
	}
	dyn := dynamicpb.NewMessage(env.Desc)
	if err := proto.Unmarshal(cm.Raw, dyn); err != nil {
		cm.Err = err
		return
	}
	which := dyn.WhichOneof(env.ContentOneof)
	if which == nil {
		cm.Err = errors.New("envelope oneof not set")
		return
	}

	switch env.style {
	case styleAnyWrapped:
		r.extractAny(cm, env, dyn, which)
	case styleNested:
		r.extractNested(cm, dyn, which)
	default:
		cm.Err = errors.New("unknown envelope style")
	}
}

func (r *Registry) extractAny(cm *capture.CapturedMessage, env *Envelope, dyn protoreflect.Message, which protoreflect.FieldDescriptor) {
	inner := dyn.Get(which).Message()
	anyField, ok := env.AnyFieldByOption[which.Number()]
	if !ok {
		cm.Err = errors.New("no Any field for oneof option")
		return
	}
	anyMsg := inner.Get(anyField).Message()
	cm.TypeUrl = anyMsg.Get(env.AnyTypeUrlField).String()
	raw := anyMsg.Get(env.AnyValueField).Bytes()
	cm.InnerRaw = append([]byte(nil), raw...)
}

// extractNested descends through nested oneofs until it finds a leaf message
// (one whose descriptor has no oneof, or whose active oneof branch is not a
// further message). The leaf's FullName is the TypeUrl; its re-serialized
// bytes are InnerRaw.
func (r *Registry) extractNested(cm *capture.CapturedMessage, dyn protoreflect.Message, which protoreflect.FieldDescriptor) {
	curMsg := dyn.Get(which).Message()
	curDesc := which.Message()
	for curDesc.Oneofs().Len() > 0 {
		oneof := curDesc.Oneofs().Get(0)
		next := curMsg.WhichOneof(oneof)
		if next == nil || next.Kind() != protoreflect.MessageKind {
			break
		}
		curMsg = curMsg.Get(next).Message()
		curDesc = next.Message()
	}
	cm.TypeUrl = string(curDesc.FullName())
	leafBytes, err := proto.Marshal(curMsg.Interface())
	if err != nil {
		cm.Err = err
		return
	}
	cm.InnerRaw = leafBytes
}

func walkMessages(ms protoreflect.MessageDescriptors, fn func(protoreflect.MessageDescriptor)) {
	for i := 0; i < ms.Len(); i++ {
		md := ms.Get(i)
		fn(md)
		walkMessages(md.Messages(), fn)
	}
}
