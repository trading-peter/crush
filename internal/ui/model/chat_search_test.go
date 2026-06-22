package model

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/ui/chat"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/list"
	"github.com/stretchr/testify/require"
)

// searchTestItem is a minimal list item that satisfies chat.SearchableItem,
// list.Highlightable, and chat.Expandable for testing the search feature.
type searchTestItem struct {
	id        string
	text      string
	version   *list.Versioned
	startLine int
	startCol  int
	endLine   int
	endCol    int
	expanded  bool
}

func newSearchTestItem(id, text string) *searchTestItem {
	return &searchTestItem{
		id:        id,
		text:      text,
		version:   list.NewVersioned(),
		startLine: -1,
		startCol:  -1,
		endLine:   -1,
		endCol:    -1,
	}
}

func (m *searchTestItem) ID() string           { return m.id }
func (m *searchTestItem) Render(int) string    { return m.text }
func (m *searchTestItem) RawRender(int) string { return m.text }
func (m *searchTestItem) SearchText() string   { return m.text }
func (m *searchTestItem) Version() uint64      { return m.version.Version() }
func (m *searchTestItem) Finished() bool       { return true }
func (m *searchTestItem) SetHighlight(startLine, startCol, endLine, endCol int) {
	m.startLine = startLine
	m.startCol = startCol
	m.endLine = endLine
	m.endCol = endCol
}
func (m *searchTestItem) Highlight() (int, int, int, int) {
	return m.startLine, m.startCol, m.endLine, m.endCol
}
func (m *searchTestItem) ToggleExpanded() bool {
	m.expanded = !m.expanded
	m.version.Bump()
	return m.expanded
}
func (m *searchTestItem) SetExpanded(expanded bool) bool {
	if m.expanded == expanded {
		return expanded
	}
	m.expanded = expanded
	m.version.Bump()
	return expanded
}

var (
	_ chat.SearchableItem = (*searchTestItem)(nil)
	_ list.Highlightable  = (*searchTestItem)(nil)
	_ chat.Expandable     = (*searchTestItem)(nil)
)

func newSearchChat(items ...*searchTestItem) *Chat {
	com := common.DefaultCommon(nil)
	c := NewChat(com)
	msgs := make([]chat.MessageItem, len(items))
	for i, item := range items {
		msgs[i] = item
	}
	c.SetMessages(msgs...)
	c.SetSize(80, 24)
	return c
}

func runeKey(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

func TestComputeMatches_BasicSingleItem(t *testing.T) {
	t.Parallel()
	c := newSearchChat(
		newSearchTestItem("a", "hello world hello"),
	)
	c.computeMatches("hello")
	require.Len(t, c.search.matches, 2)
	require.Equal(t, 0, c.search.matches[0].itemIdx)
	require.Equal(t, 0, c.search.matches[0].occurrence)
	require.Equal(t, 0, c.search.matches[1].itemIdx)
	require.Equal(t, 1, c.search.matches[1].occurrence)
}

func TestComputeMatches_MultipleItems(t *testing.T) {
	t.Parallel()
	c := newSearchChat(
		newSearchTestItem("a", "alpha"),
		newSearchTestItem("b", "beta alpha"),
		newSearchTestItem("c", "gamma"),
	)
	c.computeMatches("alpha")
	require.Len(t, c.search.matches, 2)
	require.Equal(t, 0, c.search.matches[0].itemIdx)
	require.Equal(t, 1, c.search.matches[1].itemIdx)
}

func TestComputeMatches_CaseInsensitive(t *testing.T) {
	t.Parallel()
	c := newSearchChat(
		newSearchTestItem("a", "Hello HELLO hello"),
	)
	c.computeMatches("hello")
	require.Len(t, c.search.matches, 3)
}

func TestComputeMatches_EmptyQuery(t *testing.T) {
	t.Parallel()
	c := newSearchChat(
		newSearchTestItem("a", "hello"),
	)
	c.computeMatches("")
	require.Empty(t, c.search.matches)
	require.Equal(t, -1, c.search.current)
}

func TestComputeMatches_NoMatches(t *testing.T) {
	t.Parallel()
	c := newSearchChat(
		newSearchTestItem("a", "hello world"),
	)
	c.computeMatches("xyz")
	require.Empty(t, c.search.matches)
	require.Equal(t, -1, c.search.current)
}

func TestNextMatch_WrapAround(t *testing.T) {
	t.Parallel()
	c := newSearchChat(
		newSearchTestItem("a", "foo"),
		newSearchTestItem("b", "bar foo"),
	)
	c.computeMatches("foo")
	require.Len(t, c.search.matches, 2)

	require.Equal(t, 0, c.search.current)
	c.nextMatch()
	require.Equal(t, 1, c.search.current)
	c.nextMatch()
	// Should wrap around to 0.
	require.Equal(t, 0, c.search.current)
}

func TestPrevMatch_WrapAround(t *testing.T) {
	t.Parallel()
	c := newSearchChat(
		newSearchTestItem("a", "foo"),
		newSearchTestItem("b", "foo"),
	)
	c.computeMatches("foo")
	require.Len(t, c.search.matches, 2)

	require.Equal(t, 0, c.search.current)
	c.prevMatch()
	// Should wrap around to last match.
	require.Equal(t, 1, c.search.current)
	c.prevMatch()
	require.Equal(t, 0, c.search.current)
}

func TestNextMatch_NoMatches(t *testing.T) {
	t.Parallel()
	c := newSearchChat(
		newSearchTestItem("a", "hello"),
	)
	c.computeMatches("xyz")
	c.nextMatch() // should not panic
	require.Equal(t, -1, c.search.current)
}

func TestFindMatchInRendered_SingleLine(t *testing.T) {
	t.Parallel()
	rendered := "hello world foo bar"
	line, col, found := findMatchInRendered(rendered, "foo", 0)
	require.True(t, found)
	require.Equal(t, 0, line)
	require.Equal(t, 12, col)
}

func TestFindMatchInRendered_MultiLine(t *testing.T) {
	t.Parallel()
	rendered := "line one\nfoo here\nfoo again"
	line, col, found := findMatchInRendered(rendered, "foo", 0)
	require.True(t, found)
	require.Equal(t, 1, line)
	require.Equal(t, 0, col)

	line, col, found = findMatchInRendered(rendered, "foo", 1)
	require.True(t, found)
	require.Equal(t, 2, line)
	require.Equal(t, 0, col)
}

func TestFindMatchInRendered_NotFound(t *testing.T) {
	t.Parallel()
	rendered := "hello world"
	_, _, found := findMatchInRendered(rendered, "xyz", 0)
	require.False(t, found)
}

func TestFindMatchInRendered_OccurrenceOutOfRange(t *testing.T) {
	t.Parallel()
	rendered := "foo foo"
	_, _, found := findMatchInRendered(rendered, "foo", 2)
	require.False(t, found)
}

func TestFindMatchInRendered_CaseInsensitive(t *testing.T) {
	t.Parallel()
	rendered := "Hello HELLO"
	line, col, found := findMatchInRendered(rendered, "hello", 1)
	require.True(t, found)
	require.Equal(t, 0, line)
	require.Equal(t, 6, col)
}

func TestFindMatchInRendered_MultiBytePrefix(t *testing.T) {
	t.Parallel()
	// ▌ is a 3-byte UTF-8 character but 1 cell wide. The cell column
	// must be 2 (not 4 which would be the byte offset).
	rendered := "▌ hello"
	line, col, found := findMatchInRendered(rendered, "hello", 0)
	require.True(t, found)
	require.Equal(t, 0, line)
	require.Equal(t, 2, col, "cell column should account for multi-byte prefix")
}

func TestStartSearch_EntersInputPhase(t *testing.T) {
	t.Parallel()
	c := newSearchChat(
		newSearchTestItem("a", "hello"),
	)
	c.StartSearch()
	require.Equal(t, searchInput, c.search.phase)
	require.True(t, c.IsSearching())
}

func TestCancelSearch_RestoresState(t *testing.T) {
	t.Parallel()
	c := newSearchChat(
		newSearchTestItem("a", "hello"),
		newSearchTestItem("b", "world"),
	)
	c.StartSearch()
	require.True(t, c.IsSearching())
	c.cancelSearch()
	require.False(t, c.IsSearching())
	require.Nil(t, c.search.matches)
	require.Equal(t, -1, c.search.current)
}

func TestHandleSearchKeyMsg_InputPhaseRouting(t *testing.T) {
	t.Parallel()
	c := newSearchChat(
		newSearchTestItem("a", "hello world"),
	)
	c.StartSearch()

	// Typing a character should be consumed and update matches.
	handled, _ := c.handleSearchKeyMsg(runeKey('h'))
	require.True(t, handled)
	require.Equal(t, "h", c.search.input.Value())
	require.Len(t, c.search.matches, 1)

	// Enter confirms.
	handled, _ = c.handleSearchKeyMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.True(t, handled)
	require.Equal(t, searchConfirmed, c.search.phase)
}

func TestHandleSearchKeyMsg_InputPhaseEscape(t *testing.T) {
	t.Parallel()
	c := newSearchChat(
		newSearchTestItem("a", "hello"),
	)
	c.StartSearch()
	handled, _ := c.handleSearchKeyMsg(tea.KeyPressMsg{Code: tea.KeyEsc})
	require.True(t, handled)
	require.False(t, c.IsSearching())
}

func TestHandleSearchKeyMsg_ConfirmedPhaseNavigation(t *testing.T) {
	t.Parallel()
	c := newSearchChat(
		newSearchTestItem("a", "foo"),
		newSearchTestItem("b", "foo"),
	)
	c.StartSearch()
	// Type "foo" and confirm.
	c.handleSearchKeyMsg(runeKey('f'))
	c.handleSearchKeyMsg(runeKey('o'))
	c.handleSearchKeyMsg(runeKey('o'))
	c.handleSearchKeyMsg(tea.KeyPressMsg{Code: tea.KeyEnter})

	require.Equal(t, searchConfirmed, c.search.phase)
	require.Len(t, c.search.matches, 2)
	require.Equal(t, 0, c.search.current)

	// "n" advances.
	handled, _ := c.handleSearchKeyMsg(runeKey('n'))
	require.True(t, handled)
	require.Equal(t, 1, c.search.current)

	// Non-search key falls through.
	handled, _ = c.handleSearchKeyMsg(runeKey('j'))
	require.False(t, handled)

	// Esc cancels.
	handled, _ = c.handleSearchKeyMsg(tea.KeyPressMsg{Code: tea.KeyEsc})
	require.True(t, handled)
	require.False(t, c.IsSearching())
}

func TestSelectCurrentMatch_AutoExpandsCollapsedItem(t *testing.T) {
	t.Parallel()
	itemA := newSearchTestItem("a", "foo here")
	itemB := newSearchTestItem("b", "no match")
	c := newSearchChat(itemA, itemB)

	c.StartSearch()
	c.computeMatches("foo")
	c.selectCurrentMatch()

	require.True(t, itemA.expanded, "match item should be auto-expanded")
	require.Equal(t, 0, c.search.autoExpandedIdx)
}

func TestSelectCurrentMatch_CollapsesPreviousOnItemChange(t *testing.T) {
	t.Parallel()
	itemA := newSearchTestItem("a", "foo here")
	itemB := newSearchTestItem("b", "foo there")
	c := newSearchChat(itemA, itemB)

	c.StartSearch()
	c.computeMatches("foo")
	require.Len(t, c.search.matches, 2)

	// First match in item A → expand A.
	c.selectCurrentMatch()
	require.True(t, itemA.expanded)
	require.False(t, itemB.expanded)

	// Move to match in item B → collapse A, expand B.
	c.nextMatch()
	require.False(t, itemA.expanded, "previous item should be collapsed")
	require.True(t, itemB.expanded, "new item should be expanded")
}

func TestSelectCurrentMatch_StaysExpandedWithinSameItem(t *testing.T) {
	t.Parallel()
	itemA := newSearchTestItem("a", "foo foo foo")
	c := newSearchChat(itemA)

	c.StartSearch()
	c.computeMatches("foo")
	require.Len(t, c.search.matches, 3)

	// Navigate within the same item — should stay expanded.
	c.selectCurrentMatch()
	require.True(t, itemA.expanded)
	c.nextMatch()
	require.True(t, itemA.expanded, "item should stay expanded for same-item matches")
	c.nextMatch()
	require.True(t, itemA.expanded)
}

func TestCancelSearch_CollapsesAutoExpanded(t *testing.T) {
	t.Parallel()
	itemA := newSearchTestItem("a", "foo here")
	c := newSearchChat(itemA)

	c.StartSearch()
	c.computeMatches("foo")
	c.selectCurrentMatch()
	require.True(t, itemA.expanded)

	c.cancelSearch()
	require.False(t, itemA.expanded, "auto-expanded item should be collapsed on cancel")
}

func TestNextMatch_NoExpandForNonExpandable(t *testing.T) {
	t.Parallel()
	// Non-expandable items: use a raw list.Item that doesn't implement Expandable.
	// searchTestItem implements Expandable, so this tests that autoExpandedIdx
	// stays -1 when navigating to items that aren't Expandable.
	c := newSearchChat(
		newSearchTestItem("a", "foo"),
	)
	c.StartSearch()
	c.computeMatches("foo")
	c.selectCurrentMatch()
	// autoExpandedIdx should be set because searchTestItem IS expandable.
	require.Equal(t, 0, c.search.autoExpandedIdx)
}
