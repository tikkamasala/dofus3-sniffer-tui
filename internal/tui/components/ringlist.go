package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"sniffer-tui/internal/capture"
	"sniffer-tui/internal/util"
)

// Item wraps a CapturedMessage with a monotonic sequence number so the
// virtualized view can prune filtered entries evicted by the ring.
type Item struct {
	Seq uint64
	Msg capture.CapturedMessage
}

type FriendlyFunc func(typeUrl string) string

// RingList is a bounded, virtualized list of captured messages. Append is
// O(1) amortized; View only renders the visible viewport.
type RingList struct {
	ring      *util.Ring[*Item]
	filter    string
	blacklist map[string]struct{}
	friendly  FriendlyFunc

	// visible is the filtered view in oldest-to-newest order.
	visible   []*Item
	selected  int
	scrollTop int

	width  int
	height int

	styleHeader   lipgloss.Style
	styleRow      lipgloss.Style
	styleSelected lipgloss.Style
	styleClient   lipgloss.Style
	styleServer   lipgloss.Style
}

func NewRingList(capacity int, friendly FriendlyFunc) *RingList {
	return &RingList{
		ring:      util.NewRing[*Item](capacity),
		blacklist: map[string]struct{}{},
		friendly:  friendly,
		styleHeader: lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Bold(true),
		styleRow: lipgloss.NewStyle(),
		styleSelected: lipgloss.NewStyle().
			Background(lipgloss.Color("238")).
			Foreground(lipgloss.Color("230")),
		styleClient: lipgloss.NewStyle().Foreground(lipgloss.Color("39")),
		styleServer: lipgloss.NewStyle().Foreground(lipgloss.Color("208")),
	}
}

func (l *RingList) SetSize(w, h int) {
	l.width = w
	l.height = h
	l.clampScroll()
}

func (l *RingList) SetFriendly(fn FriendlyFunc) {
	if fn == nil {
		fn = func(s string) string { return s }
	}
	l.friendly = fn
}

func (l *RingList) SetFilter(s string) {
	if s == l.filter {
		return
	}
	l.filter = s
	l.refilter()
}

func (l *RingList) Filter() string { return l.filter }

func (l *RingList) BlacklistCount() int { return len(l.blacklist) }

// BlacklistVisible adds every distinct TypeUrl in the current visible set to
// the blacklist, then re-filters.
func (l *RingList) BlacklistVisible() int {
	added := 0
	for _, it := range l.visible {
		if it.Msg.TypeUrl == "" {
			continue
		}
		if _, ok := l.blacklist[it.Msg.TypeUrl]; !ok {
			l.blacklist[it.Msg.TypeUrl] = struct{}{}
			added++
		}
	}
	l.refilter()
	return added
}

func (l *RingList) ClearBlacklist() {
	if len(l.blacklist) == 0 {
		return
	}
	l.blacklist = map[string]struct{}{}
	l.refilter()
}

// Clear discards every captured message, preserving filter and blacklist.
// Returns the number of messages removed.
func (l *RingList) Clear() int {
	n := l.ring.Len()
	l.ring = util.NewRing[*Item](l.ring.Cap())
	l.visible = l.visible[:0]
	l.selected = 0
	l.scrollTop = 0
	return n
}

func (l *RingList) Append(m capture.CapturedMessage) {
	oldOldest := l.ring.OldestSeq()
	seq := l.ring.Append(&Item{Msg: m})
	// The Ring only stores the pointer; re-fetch to set Seq now that we know it.
	item := l.ring.At(l.ring.Len() - 1)
	item.Seq = seq

	newOldest := l.ring.OldestSeq()
	if newOldest > oldOldest {
		// Prune evicted items from the visible window.
		cut := 0
		for cut < len(l.visible) && l.visible[cut].Seq < newOldest {
			cut++
		}
		if cut > 0 {
			l.visible = l.visible[cut:]
			l.selected -= cut
			if l.selected < 0 {
				l.selected = 0
			}
			l.scrollTop -= cut
			if l.scrollTop < 0 {
				l.scrollTop = 0
			}
		}
	}

	if l.matches(item) {
		l.visible = append(l.visible, item)
	}
	l.clampScroll()
}

func (l *RingList) refilter() {
	prevSel := -1
	if l.selected < len(l.visible) {
		prevSel = int(l.visible[l.selected].Seq)
	}
	l.visible = l.visible[:0]
	for i := 0; i < l.ring.Len(); i++ {
		it := l.ring.At(i)
		if l.matches(it) {
			l.visible = append(l.visible, it)
		}
	}
	l.selected = 0
	if prevSel >= 0 {
		for i, it := range l.visible {
			if int(it.Seq) == prevSel {
				l.selected = i
				break
			}
		}
	}
	l.scrollTop = 0
	l.clampScroll()
}

func (l *RingList) matches(it *Item) bool {
	m := it.Msg
	if _, blocked := l.blacklist[m.TypeUrl]; blocked {
		return false
	}
	if l.filter == "" {
		return true
	}
	needle := strings.ToLower(l.filter)
	friendly := m.TypeUrl
	if l.friendly != nil {
		friendly = l.friendly(m.TypeUrl)
	}
	return strings.Contains(strings.ToLower(friendly), needle) ||
		strings.Contains(strings.ToLower(m.TypeUrl), needle)
}

func (l *RingList) Selected() *Item {
	if l.selected < 0 || l.selected >= len(l.visible) {
		return nil
	}
	return l.visible[l.selected]
}

func (l *RingList) Visible() []*Item { return l.visible }

func (l *RingList) MoveUp() {
	if l.selected > 0 {
		l.selected--
	}
	l.ensureVisible()
}

func (l *RingList) MoveDown() {
	if l.selected < len(l.visible)-1 {
		l.selected++
	}
	l.ensureVisible()
}

func (l *RingList) PageUp() {
	l.selected -= l.bodyHeight()
	if l.selected < 0 {
		l.selected = 0
	}
	l.ensureVisible()
}

func (l *RingList) PageDown() {
	l.selected += l.bodyHeight()
	if l.selected >= len(l.visible) {
		l.selected = len(l.visible) - 1
	}
	l.ensureVisible()
}

func (l *RingList) Home() {
	l.selected = 0
	l.ensureVisible()
}

func (l *RingList) End() {
	l.selected = len(l.visible) - 1
	if l.selected < 0 {
		l.selected = 0
	}
	l.ensureVisible()
}

// ScrollBy moves the viewport by n rows without moving the selection cursor.
// Positive n scrolls down (toward newer messages), negative up.
func (l *RingList) ScrollBy(n int) {
	l.scrollTop += n
	l.clampScroll()
}

func (l *RingList) bodyHeight() int {
	h := l.height - 1 // header takes 1 row
	if h < 1 {
		return 1
	}
	return h
}

func (l *RingList) ensureVisible() {
	body := l.bodyHeight()
	if l.selected < l.scrollTop {
		l.scrollTop = l.selected
	}
	if l.selected >= l.scrollTop+body {
		l.scrollTop = l.selected - body + 1
	}
	l.clampScroll()
}

func (l *RingList) clampScroll() {
	body := l.bodyHeight()
	max := len(l.visible) - body
	if max < 0 {
		max = 0
	}
	if l.scrollTop > max {
		l.scrollTop = max
	}
	if l.scrollTop < 0 {
		l.scrollTop = 0
	}
}

func (l *RingList) View() string {
	if l.width <= 0 || l.height <= 0 {
		return ""
	}
	const timeCol = 12
	const srcCol = 6
	const kindCol = 4
	const sizeCol = 7
	nameCol := l.width - timeCol - srcCol - kindCol - sizeCol - 4
	if nameCol < 8 {
		nameCol = 8
	}

	header := l.styleHeader.Render(
		pad("Time", timeCol) + " " +
			pad("Src", srcCol) + " " +
			pad("Kind", kindCol) + " " +
			pad("TypeURL", nameCol) + " " +
			padRight("Size", sizeCol),
	)

	body := l.bodyHeight()
	var rows []string
	rows = append(rows, header)
	end := l.scrollTop + body
	if end > len(l.visible) {
		end = len(l.visible)
	}
	for i := l.scrollTop; i < end; i++ {
		it := l.visible[i]
		row := l.renderRow(it, timeCol, srcCol, kindCol, nameCol, sizeCol)
		if i == l.selected {
			row = l.styleSelected.Render(row)
		}
		rows = append(rows, row)
	}
	for len(rows) < l.height {
		rows = append(rows, "")
	}
	return strings.Join(rows, "\n")
}

func (l *RingList) renderRow(it *Item, timeCol, srcCol, kindCol, nameCol, sizeCol int) string {
	m := it.Msg
	ts := m.Time.Format("15:04:05.000")
	src := m.Dir.String()
	kind := m.Kind.String()
	name := m.TypeUrl
	if l.friendly != nil && m.TypeUrl != "" {
		name = l.friendly(m.TypeUrl)
	}
	if m.TypeUrl == "" {
		if m.Err != nil {
			name = "<err: " + m.Err.Error() + ">"
		} else {
			name = "<no envelope>"
		}
	}
	size := fmt.Sprintf("%d", m.Size)
	var srcStyled string
	if m.Dir == capture.ServerToClient {
		srcStyled = l.styleServer.Render(padRight(src, srcCol))
	} else {
		srcStyled = l.styleClient.Render(padRight(src, srcCol))
	}
	return pad(ts, timeCol) + " " + srcStyled + " " + pad(kind, kindCol) + " " + pad(name, nameCol) + " " + padLeft(size, sizeCol)
}

func pad(s string, w int) string {
	if len(s) > w {
		return s[:w]
	}
	return s + strings.Repeat(" ", w-len(s))
}

func padRight(s string, w int) string { return pad(s, w) }

func padLeft(s string, w int) string {
	if len(s) > w {
		return s[:w]
	}
	return strings.Repeat(" ", w-len(s)) + s
}
