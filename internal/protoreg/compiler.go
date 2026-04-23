package protoreg

import (
	"context"
	"path/filepath"

	"github.com/bufbuild/protocompile"
	"github.com/bufbuild/protocompile/linker"
)

// Compile parses the given .proto files using bufbuild/protocompile. Each
// file's containing directory is added to the import path set so cross-file
// imports resolve. Standard imports (google/protobuf/any.proto, etc.) are
// resolved from the stdlib bundled with protocompile.
func Compile(ctx context.Context, paths []string) (linker.Files, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	dirSet := map[string]struct{}{}
	names := make([]string, 0, len(paths))
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			return nil, err
		}
		dirSet[filepath.Dir(abs)] = struct{}{}
		names = append(names, filepath.Base(abs))
	}
	importDirs := make([]string, 0, len(dirSet))
	for d := range dirSet {
		importDirs = append(importDirs, d)
	}
	c := protocompile.Compiler{
		Resolver: protocompile.WithStandardImports(&protocompile.SourceResolver{
			ImportPaths: importDirs,
		}),
		SourceInfoMode: protocompile.SourceInfoStandard,
	}
	return c.Compile(ctx, names...)
}
