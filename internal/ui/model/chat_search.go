package model

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/ui/chat"
	"github.com/charmbracelet/crush/internal/ui/list"
	"github.com/charmbracelet/x/ansi"
)

// searchPhase tracks the two-phase search interaction.
type searchPhase uint8

const (
	searchIdle      searchPhase = iota
	searchInput                 // User is typing the query; textinput is focused.
	searchConfirmed             // Query is confirmed; n/N navigate between matches.
)

// searchMatch represents a single occurrence of the query within an item.
// occurrence is the 0-based index of this match inside the item's
// SearchText(), used to re-find the exact position in the rendered output at
// draw time (rendered line/col offsets shift with terminal width, so we don't
// cache them — only the width-independent occurrence count is stored).
type searchMatch struct {
	itemIdx    int
	occurrence int
}

// searchState holds all mutable state for the in-chat text search feature.
type searchState struct {
	phase           searchPhase
	input           textinput.Model
	matches         []searchMatch
	current         int    // Index into matches; -1 when there are no matches.
	query           string // The confirmed query (frozen after Enter).
	prevSelected    int    // Selection to restore on cancel.
	autoExpandedIdx int    // Item index auto-expanded by search, or -1.
}

// searchHeight is the vertical space (in rows) consumed by the search bar.
const searchHeight = 1

// activeQuery returns the query string currently in effect — the live input
// value while typing, or the frozen query after confirmation.
func (s *searchState) activeQuery() string {
	if s.phase == searchInput {
		return s.input.Value()
	}
	return s.query
}

// computeMatches rebuilds the flat match list for the given query by scanning
// every item's SearchText().  The result is width-independent (it operates on
// plain text, not rendered output), so terminal resizes never invalidate it.
func (m *Chat) computeMatches(query string) {
	m.search.matches = m.search.matches[:0]
	m.search.current = -1
	if query == "" {
		return
	}
	q := strings.ToLower(query)
	for i := range m.list.Len() {
		item, ok := m.list.ItemAt(i).(chat.SearchableItem)
		if !ok {
			continue
		}
		haystack := strings.ToLower(item.SearchText())
		start := 0
		occ := 0
		for {
			rel := strings.Index(haystack[start:], q)
			if rel == -1 {
				break
			}
			m.search.matches = append(m.search.matches, searchMatch{
				itemIdx:    i,
				occurrence: occ,
			})
			start += rel + len(q)
			occ++
		}
	}
	if len(m.search.matches) > 0 {
		m.search.current = 0
	}
}

// selectCurrentMatch moves the list selection to the current match's item and
// scrolls it into view.  If the item is Expandable it is auto-expanded so the
// match is visible; the previously auto-expanded item (if different) is
// collapsed back to its prior state.
func (m *Chat) selectCurrentMatch() {
	if m.search.current < 0 || m.search.current >= len(m.search.matches) {
		return
	}
	match := m.search.matches[m.search.current]

	itemChanged := false

	// Collapse the previously auto-expanded item when moving to a different
	// item.
	if m.search.autoExpandedIdx >= 0 && m.search.autoExpandedIdx != match.itemIdx {
		if exp, ok := m.list.ItemAt(m.search.autoExpandedIdx).(chat.Expandable); ok {
			exp.SetExpanded(false)
		}
		m.search.autoExpandedIdx = -1
		itemChanged = true
	}

	// Auto-expand the current match's item so the match is visible.
	if exp, ok := m.list.ItemAt(match.itemIdx).(chat.Expandable); ok {
		if m.search.autoExpandedIdx != match.itemIdx {
			exp.SetExpanded(true)
			m.search.autoExpandedIdx = match.itemIdx
			itemChanged = true
		}
	}

	m.SetSelected(match.itemIdx)

	if itemChanged {
		// After expanding/collapsing, item heights changed. ScrollToSelected
		// has a no-op case for items whose first line is "in view" — but
		// the match may be deep inside the now-expanded item, off-screen.
		// ScrollToIndex puts the item at the top, guaranteeing the start of
		// the expanded content is visible.
		m.ScrollToIndex(match.itemIdx)
	} else {
		m.ScrollToSelected()
	}
}

// findMatchInRendered locates the occurrence-th instance of query in the
// ANSI-stripped, line-split rendered text and returns its (line, col)
// position in CELL coordinates (display columns, not byte offsets).
// RawRender returns content-only text without the border/padding prefix,
// so the returned column is relative to the first content character.
// Returns found == false if the occurrence doesn't exist.
func findMatchInRendered(rendered, query string, occurrence int) (line, col int, found bool) {
	if query == "" {
		return 0, 0, false
	}
	stripped := ansi.Strip(rendered)
	q := strings.ToLower(query)
	count := 0
	for li, ln := range strings.Split(stripped, "\n") {
		lower := strings.ToLower(ln)
		start := 0
		for {
			rel := strings.Index(lower[start:], q)
			if rel == -1 {
				break
			}
			if count == occurrence {
				// Convert the byte offset to a cell (display) column so
				// it lines up with HighlightBuffer's cell positioning.
				return li, ansi.StringWidth(lower[:start+rel]), true
			}
			start += rel + len(q)
			count++
		}
	}
	return 0, 0, false
}

// applySearchHighlight is a list.RenderCallback that highlights the current
// match within its item.  For all other items it clears any residual
// highlight so that search overrides mouse-drag selection while active.
func (m *Chat) applySearchHighlight(idx, _ int, item list.Item) list.Item {
	hi, ok := item.(list.Highlightable)
	if !ok {
		return item
	}

	if len(m.search.matches) == 0 || m.search.current < 0 {
		hi.SetHighlight(-1, -1, -1, -1)
		return hi.(list.Item)
	}

	current := m.search.matches[m.search.current]
	if current.itemIdx != idx {
		hi.SetHighlight(-1, -1, -1, -1)
		return hi.(list.Item)
	}

	query := m.search.activeQuery()

	var rendered string
	if rr, ok := item.(list.RawRenderable); ok {
		rendered = rr.RawRender(m.list.Width())
	} else {
		rendered = item.Render(m.list.Width())
	}

	line, col, found := findMatchInRendered(rendered, query, current.occurrence)
	if !found {
		hi.SetHighlight(-1, -1, -1, -1)
	} else {
		// findMatchInRendered returns content-space columns (relative to
		// the content text without border/padding). SetHighlight expects
		// viewport-space columns and subtracts MessageLeftPaddingTotal
		// internally, so we add it back here.
		offset := chat.MessageLeftPaddingTotal
		queryWidth := ansi.StringWidth(query)
		hi.SetHighlight(line, col+offset, line, col+queryWidth+offset)
	}
	return hi.(list.Item)
}

// startSearch initialises the search bar and enters the input phase.
func (m *Chat) startSearch() {
	m.search.phase = searchInput
	m.search.prevSelected = m.list.Selected()
	m.search.input.Focus()
	m.search.input.SetValue("")
	m.shrinkListForSearch()
}

// confirmSearch freezes the query and enters the navigation phase.
func (m *Chat) confirmSearch() {
	m.search.query = m.search.input.Value()
	m.search.phase = searchConfirmed
	m.search.input.Blur()
}

// cancelSearch clears all search state and restores the prior selection.
func (m *Chat) cancelSearch() {
	// Collapse any item that was auto-expanded by search.
	if m.search.autoExpandedIdx >= 0 {
		if exp, ok := m.list.ItemAt(m.search.autoExpandedIdx).(chat.Expandable); ok {
			exp.SetExpanded(false)
		}
		m.search.autoExpandedIdx = -1
	}

	m.search.phase = searchIdle
	m.search.matches = nil
	m.search.current = -1
	m.search.query = ""
	m.search.input.Blur()
	if m.search.prevSelected >= 0 && m.search.prevSelected < m.list.Len() {
		m.SetSelected(m.search.prevSelected)
	}
	m.restoreListAfterSearch()
}

// nextMatch advances to the next match, wrapping around.
func (m *Chat) nextMatch() {
	if len(m.search.matches) == 0 {
		return
	}
	m.search.current = (m.search.current + 1) % len(m.search.matches)
	m.selectCurrentMatch()
}

// prevMatch advances to the previous match, wrapping around.
func (m *Chat) prevMatch() {
	if len(m.search.matches) == 0 {
		return
	}
	m.search.current = (m.search.current - 1 + len(m.search.matches)) % len(m.search.matches)
	m.selectCurrentMatch()
}

// handleSearchKeyMsg processes key events while search is active.  Returns
// handled == true when the key was consumed by search and should not reach
// normal chat key handling.
//
// In the input phase every key is consumed (routed to the textinput, Enter,
// or Esc).  In the confirmed phase only search-specific keys (n, N, Esc, /)
// are consumed; everything else falls through so the user can still scroll.
func (m *Chat) handleSearchKeyMsg(msg tea.KeyMsg) (handled bool, cmd tea.Cmd) {
	switch m.search.phase {
	case searchInput:
		switch msg.String() {
		case "enter":
			m.confirmSearch()
			return true, nil
		case "esc", "alt+esc":
			m.cancelSearch()
			return true, nil
		default:
			m.search.input, cmd = m.search.input.Update(msg)
			m.computeMatches(m.search.input.Value())
			m.selectCurrentMatch()
			return true, cmd
		}

	case searchConfirmed:
		switch msg.String() {
		case "n":
			m.nextMatch()
			return true, nil
		case "N", "shift+n":
			m.prevMatch()
			return true, nil
		case "esc", "alt+esc":
			m.cancelSearch()
			return true, nil
		case "/":
			m.search.phase = searchInput
			m.search.input.Focus()
			return true, nil
		default:
			return false, nil
		}
	}

	return false, nil
}
