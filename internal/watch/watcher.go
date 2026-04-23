package watch

import (
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"sniffer-tui/internal/util"
)

// Kind discriminates which file group changed. Consumers typically react by
// recompiling the matching source (proto descriptors or mappings).
type Kind uint8

const (
	KindProto Kind = iota
	KindMapping
)

// Watcher watches two disjoint sets of file paths and fires a debounced
// callback per kind when any file in that set changes. The watcher resolves
// symlinks and watches each file's parent directory, so editors that rename-
// and-replace still trigger reloads.
type Watcher struct {
	fw           *fsnotify.Watcher
	mu           sync.Mutex
	protos       map[string]struct{}
	mappings     map[string]struct{}
	dirsProtos   map[string]int
	dirsMappings map[string]int
	onChange     func(Kind)
	protoDeb     *util.Debouncer
	mappingDeb   *util.Debouncer
	stop         chan struct{}
	done         chan struct{}
}

func New(onChange func(Kind)) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &Watcher{
		fw:           fw,
		protos:       map[string]struct{}{},
		mappings:     map[string]struct{}{},
		dirsProtos:   map[string]int{},
		dirsMappings: map[string]int{},
		onChange:     onChange,
		stop:         make(chan struct{}),
		done:         make(chan struct{}),
	}
	w.protoDeb = util.NewDebouncer(250*time.Millisecond, func() { w.onChange(KindProto) })
	w.mappingDeb = util.NewDebouncer(250*time.Millisecond, func() { w.onChange(KindMapping) })
	go w.run()
	return w, nil
}

func (w *Watcher) Set(kind Kind, paths []string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	current := w.protos
	dirs := w.dirsProtos
	if kind == KindMapping {
		current = w.mappings
		dirs = w.dirsMappings
	}

	wanted := map[string]struct{}{}
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		wanted[abs] = struct{}{}
	}

	// Remove paths no longer wanted.
	for p := range current {
		if _, keep := wanted[p]; !keep {
			dir := filepath.Dir(p)
			dirs[dir]--
			if dirs[dir] <= 0 {
				delete(dirs, dir)
				_ = w.fw.Remove(dir)
			}
			delete(current, p)
		}
	}
	// Add new paths.
	for p := range wanted {
		if _, had := current[p]; had {
			continue
		}
		dir := filepath.Dir(p)
		if dirs[dir] == 0 {
			if err := w.fw.Add(dir); err != nil {
				return err
			}
		}
		dirs[dir]++
		current[p] = struct{}{}
	}
	return nil
}

func (w *Watcher) Close() error {
	close(w.stop)
	err := w.fw.Close()
	<-w.done
	w.protoDeb.Stop()
	w.mappingDeb.Stop()
	return err
}

func (w *Watcher) run() {
	defer close(w.done)
	for {
		select {
		case <-w.stop:
			return
		case ev, ok := <-w.fw.Events:
			if !ok {
				return
			}
			if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename|fsnotify.Remove) == 0 {
				continue
			}
			abs, err := filepath.Abs(ev.Name)
			if err != nil {
				continue
			}
			w.mu.Lock()
			_, isProto := w.protos[abs]
			_, isMapping := w.mappings[abs]
			w.mu.Unlock()
			if isProto {
				w.protoDeb.Trigger()
			}
			if isMapping {
				w.mappingDeb.Trigger()
			}
		case _, ok := <-w.fw.Errors:
			if !ok {
				return
			}
		}
	}
}
