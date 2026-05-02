package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const whichKeyTimeout = time.Second

type commandID string

type uiContext string

type keyPress string

type keySequence []keyPress

type commandContext struct {
	context uiContext
	model   *model
}

type commandSpec struct {
	id          commandID
	label       string
	description string
	group       string
	visible     func(commandContext) bool
	enabled     func(commandContext) bool
	run         func(*model) tea.Cmd
}

type bindingSpec struct {
	commandID    commandID
	context      uiContext
	sequence     keySequence
	canonical    bool
	discoverable bool
	displayOrder int
}

type commandRegistry struct {
	commands         []commandSpec
	commandIndex     map[commandID]commandSpec
	bindingsByCtx    map[uiContext][]bindingSpec
	bindingsByCmdCtx map[commandID]map[uiContext][]bindingSpec
}

type keyPrefixState struct {
	context    uiContext
	sequence   keySequence
	generation uint64
}

type dispatchResult struct {
	handled   bool
	needsMore bool
	commandID commandID
	cmd       tea.Cmd
}

type whichKeyEntry struct {
	key          keyPress
	label        string
	displayOrder int
}

const (
	uiContextSearch  uiContext = "search"
	uiContextConsole uiContext = "console"

	commandQuit                  commandID = "app.quit"
	commandTabNext               commandID = "tab.next"
	commandTabPrevious           commandID = "tab.previous"
	commandTabSearch             commandID = "tab.search"
	commandTabConsole            commandID = "tab.console"
	commandSearchSubmitOrOpen    commandID = "search.submit-or-open"
	commandSearchSelectionNext   commandID = "search.selection.next"
	commandSearchSelectionPrev   commandID = "search.selection.previous"
	commandSearchPageDown        commandID = "search.page-down"
	commandSearchPageUp          commandID = "search.page-up"
	commandSearchTogglePreview   commandID = "search.preview.toggle"
	commandConsoleScrollPageDown commandID = "console.page-down"
	commandConsoleScrollPageUp   commandID = "console.page-up"
)

func defaultCommandRegistry() commandRegistry {
	return mustNewCommandRegistry(defaultCommandSpecs(), defaultBindingSpecs())
}

func defaultCommandSpecs() []commandSpec {
	return []commandSpec{
		{id: commandQuit, label: "Quit", description: "Close the TUI.", group: "app", visible: alwaysVisible, enabled: alwaysEnabled, run: runQuit},
		{id: commandTabNext, label: "Next tab", description: "Switch to the next tab.", group: "tabs", visible: multipleTabsVisible, enabled: multipleTabsVisible, run: runNextTab},
		{id: commandTabPrevious, label: "Previous tab", description: "Switch to the previous tab.", group: "tabs", visible: multipleTabsVisible, enabled: multipleTabsVisible, run: runPreviousTab},
		{id: commandTabSearch, label: "Search tab", description: "Switch to search.", group: "tabs", visible: alwaysVisible, enabled: alwaysEnabled, run: runSearchTab},
		{id: commandTabConsole, label: "Console tab", description: "Switch to console logs.", group: "tabs", visible: alwaysVisible, enabled: alwaysEnabled, run: runConsoleTab},
		{id: commandSearchSubmitOrOpen, label: "Search/open", description: "Run the prompt or open the selected result.", group: "search", visible: searchTabVisible, enabled: searchTabVisible, run: runSubmitOrOpen},
		{id: commandSearchSelectionNext, label: "Next result", description: "Move selection down.", group: "search", visible: searchTabVisible, enabled: hasSearchResults, run: runMoveSelectionDown},
		{id: commandSearchSelectionPrev, label: "Previous result", description: "Move selection up.", group: "search", visible: searchTabVisible, enabled: hasSearchResults, run: runMoveSelectionUp},
		{id: commandSearchPageDown, label: "Page results down", description: "Move one result page down.", group: "search", visible: searchTabVisible, enabled: hasSearchResults, run: runPageResultsDown},
		{id: commandSearchPageUp, label: "Page results up", description: "Move one result page up.", group: "search", visible: searchTabVisible, enabled: hasSearchResults, run: runPageResultsUp},
		{id: commandSearchTogglePreview, label: "Toggle preview", description: "Show or hide the preview pane.", group: "search", visible: searchTabVisible, enabled: searchTabVisible, run: runTogglePreview},
		{id: commandConsoleScrollPageDown, label: "Page console down", description: "Scroll console history down.", group: "console", visible: consoleTabVisible, enabled: consoleTabVisible, run: runConsolePageDown},
		{id: commandConsoleScrollPageUp, label: "Page console up", description: "Scroll console history up.", group: "console", visible: consoleTabVisible, enabled: consoleTabVisible, run: runConsolePageUp},
	}
}

func defaultBindingSpecs() []bindingSpec {
	common := []bindingSpec{
		{commandID: commandQuit, sequence: keySequence{"ctrl+x", "ctrl+c"}, canonical: true, discoverable: true, displayOrder: 10},
		{commandID: commandQuit, sequence: keySequence{"ctrl+c", "ctrl+c"}, discoverable: false, displayOrder: 11},
		{commandID: commandTabNext, sequence: keySequence{"tab"}, canonical: true, discoverable: true, displayOrder: 20},
		{commandID: commandTabNext, sequence: keySequence{"ctrl+x", "o"}, discoverable: true, displayOrder: 21},
		{commandID: commandTabPrevious, sequence: keySequence{"shift+tab"}, canonical: true, discoverable: true, displayOrder: 30},
		{commandID: commandTabSearch, sequence: keySequence{"ctrl+c", "s"}, canonical: true, discoverable: true, displayOrder: 40},
		{commandID: commandTabConsole, sequence: keySequence{"ctrl+c", "c"}, canonical: true, discoverable: true, displayOrder: 50},
	}
	search := []bindingSpec{
		{commandID: commandSearchSubmitOrOpen, sequence: keySequence{"enter"}, canonical: true, discoverable: true, displayOrder: 60},
		{commandID: commandSearchSelectionNext, sequence: keySequence{"ctrl+n"}, canonical: true, discoverable: true, displayOrder: 70},
		{commandID: commandSearchSelectionNext, sequence: keySequence{"down"}, discoverable: false, displayOrder: 71},
		{commandID: commandSearchSelectionPrev, sequence: keySequence{"ctrl+p"}, canonical: true, discoverable: true, displayOrder: 80},
		{commandID: commandSearchSelectionPrev, sequence: keySequence{"up"}, discoverable: false, displayOrder: 81},
		{commandID: commandSearchPageDown, sequence: keySequence{"ctrl+v"}, canonical: true, discoverable: true, displayOrder: 90},
		{commandID: commandSearchPageDown, sequence: keySequence{"pgdown"}, discoverable: false, displayOrder: 91},
		{commandID: commandSearchPageUp, sequence: keySequence{"alt+v"}, canonical: true, discoverable: true, displayOrder: 100},
		{commandID: commandSearchPageUp, sequence: keySequence{"pgup"}, discoverable: false, displayOrder: 101},
		{commandID: commandSearchTogglePreview, sequence: keySequence{"ctrl+c", "p"}, canonical: true, discoverable: true, displayOrder: 110},
	}
	console := []bindingSpec{
		{commandID: commandConsoleScrollPageDown, sequence: keySequence{"ctrl+v"}, canonical: true, discoverable: true, displayOrder: 60},
		{commandID: commandConsoleScrollPageDown, sequence: keySequence{"pgdown"}, discoverable: false, displayOrder: 61},
		{commandID: commandConsoleScrollPageUp, sequence: keySequence{"alt+v"}, canonical: true, discoverable: true, displayOrder: 70},
		{commandID: commandConsoleScrollPageUp, sequence: keySequence{"pgup"}, discoverable: false, displayOrder: 71},
	}

	bindings := bindingsForContext(uiContextSearch, append(append([]bindingSpec{}, common...), search...))
	bindings = append(bindings, bindingsForContext(uiContextConsole, append(append([]bindingSpec{}, common...), console...))...)
	return bindings
}

func bindingsForContext(context uiContext, specs []bindingSpec) []bindingSpec {
	bindings := make([]bindingSpec, 0, len(specs))
	for _, spec := range specs {
		spec.context = context
		bindings = append(bindings, spec)
	}
	return bindings
}

func mustNewCommandRegistry(commands []commandSpec, bindings []bindingSpec) commandRegistry {
	registry, err := newCommandRegistry(commands, bindings)
	if err != nil {
		panic(err)
	}
	return registry
}

func newCommandRegistry(commands []commandSpec, bindings []bindingSpec) (commandRegistry, error) {
	registry := commandRegistry{
		commands:         append([]commandSpec(nil), commands...),
		commandIndex:     make(map[commandID]commandSpec, len(commands)),
		bindingsByCtx:    make(map[uiContext][]bindingSpec),
		bindingsByCmdCtx: make(map[commandID]map[uiContext][]bindingSpec),
	}
	for _, command := range commands {
		if strings.TrimSpace(string(command.id)) == "" {
			return commandRegistry{}, fmt.Errorf("command id is required")
		}
		if _, exists := registry.commandIndex[command.id]; exists {
			return commandRegistry{}, fmt.Errorf("duplicate command id %q", command.id)
		}
		registry.commandIndex[command.id] = command
	}
	if err := validateBindings(registry.commandIndex, bindings); err != nil {
		return commandRegistry{}, err
	}
	for _, binding := range bindings {
		registry.bindingsByCtx[binding.context] = append(registry.bindingsByCtx[binding.context], binding)
		if registry.bindingsByCmdCtx[binding.commandID] == nil {
			registry.bindingsByCmdCtx[binding.commandID] = make(map[uiContext][]bindingSpec)
		}
		registry.bindingsByCmdCtx[binding.commandID][binding.context] = append(registry.bindingsByCmdCtx[binding.commandID][binding.context], binding)
	}
	for context, contextBindings := range registry.bindingsByCtx {
		sort.SliceStable(contextBindings, func(left int, right int) bool {
			if contextBindings[left].displayOrder != contextBindings[right].displayOrder {
				return contextBindings[left].displayOrder < contextBindings[right].displayOrder
			}
			return keySequenceString(contextBindings[left].sequence) < keySequenceString(contextBindings[right].sequence)
		})
		registry.bindingsByCtx[context] = contextBindings
	}
	return registry, nil
}

func (state keyPrefixState) active() bool {
	return strings.TrimSpace(string(state.context)) != "" && len(state.sequence) > 0
}

func (state *keyPrefixState) clear() {
	state.context = ""
	state.sequence = nil
	state.generation++
}

func (state *keyPrefixState) replace(context uiContext, sequence keySequence) {
	state.context = context
	state.sequence = append(state.sequence[:0], sequence...)
	state.generation++
}

func (r commandRegistry) dispatchKey(ctx commandContext, prefix *keyPrefixState, msg tea.KeyMsg) dispatchResult {
	press, ok := normalizedKeyPress(msg)
	if !ok {
		return dispatchResult{}
	}
	if press == keyPress("ctrl+g") && prefix != nil && prefix.active() {
		prefix.clear()
		return dispatchResult{handled: true}
	}
	sequence := keySequence{press}
	if prefix != nil && prefix.active() && prefix.context == ctx.context {
		sequence = append(append(keySequence(nil), prefix.sequence...), press)
	}
	if binding, ok := r.bindingForSequence(ctx.context, sequence, ctx); ok {
		if prefix != nil {
			prefix.clear()
		}
		command, ok := r.command(binding.commandID)
		if !ok {
			return dispatchResult{handled: true}
		}
		if command.enabled != nil && !command.enabled(ctx) {
			return dispatchResult{handled: true, commandID: command.id}
		}
		if command.run == nil || ctx.model == nil {
			return dispatchResult{handled: true, commandID: command.id}
		}
		return dispatchResult{handled: true, commandID: command.id, cmd: command.run(ctx.model)}
	}
	if r.hasPrefixMatch(ctx, sequence) {
		if prefix != nil {
			prefix.replace(ctx.context, sequence)
		}
		return dispatchResult{handled: true, needsMore: true}
	}
	if prefix != nil && prefix.active() && prefix.context == ctx.context {
		prefix.clear()
		return dispatchResult{handled: true}
	}
	return dispatchResult{}
}

func (r commandRegistry) bindingForSequence(context uiContext, sequence keySequence, ctx commandContext) (bindingSpec, bool) {
	for _, binding := range r.bindingsByCtx[context] {
		if !keySequenceEqual(binding.sequence, sequence) {
			continue
		}
		command, ok := r.command(binding.commandID)
		if !ok || (command.visible != nil && !command.visible(ctx)) {
			continue
		}
		return binding, true
	}
	return bindingSpec{}, false
}

func (r commandRegistry) hasPrefixMatch(ctx commandContext, prefix keySequence) bool {
	for _, binding := range r.bindingsByCtx[ctx.context] {
		if !keySequenceStartsWith(binding.sequence, prefix) {
			continue
		}
		command, ok := r.command(binding.commandID)
		if !ok || (command.visible != nil && !command.visible(ctx)) {
			continue
		}
		return true
	}
	return false
}

func (r commandRegistry) whichKeyEntries(ctx commandContext, prefix keySequence) []whichKeyEntry {
	if len(prefix) == 0 {
		return nil
	}
	grouped := map[keyPress]whichKeyEntry{}
	for _, binding := range r.bindingsByCtx[ctx.context] {
		if !binding.discoverable || !keySequenceStartsWith(binding.sequence, prefix) {
			continue
		}
		command, ok := r.command(binding.commandID)
		if !ok {
			continue
		}
		if command.visible != nil && !command.visible(ctx) {
			continue
		}
		if command.enabled != nil && !command.enabled(ctx) {
			continue
		}
		next := binding.sequence[len(prefix)]
		entry := grouped[next]
		if entry.key == "" || binding.displayOrder < entry.displayOrder {
			entry.key = next
			entry.displayOrder = binding.displayOrder
		}
		if len(binding.sequence) == len(prefix)+1 {
			entry.label = command.label
		} else if entry.label == "" {
			entry.label = "+prefix"
		}
		grouped[next] = entry
	}
	entries := make([]whichKeyEntry, 0, len(grouped))
	for _, entry := range grouped {
		if strings.TrimSpace(entry.label) == "" {
			entry.label = "+prefix"
		}
		entries = append(entries, entry)
	}
	sort.SliceStable(entries, func(left int, right int) bool {
		if entries[left].displayOrder != entries[right].displayOrder {
			return entries[left].displayOrder < entries[right].displayOrder
		}
		return formatKeyPress(entries[left].key) < formatKeyPress(entries[right].key)
	})
	return entries
}

func (r commandRegistry) command(id commandID) (commandSpec, bool) {
	command, ok := r.commandIndex[id]
	return command, ok
}

func (r commandRegistry) formatCanonicalBinding(id commandID, context uiContext) string {
	for _, binding := range r.bindingsByCmdCtx[id][context] {
		if binding.canonical {
			return formatKeySequence(binding.sequence)
		}
	}
	return ""
}

func keySequenceEqual(left keySequence, right keySequence) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func keySequenceStartsWith(sequence keySequence, prefix keySequence) bool {
	if len(prefix) == 0 || len(prefix) >= len(sequence) {
		return false
	}
	for index := range prefix {
		if sequence[index] != prefix[index] {
			return false
		}
	}
	return true
}

func normalizedKeyPress(msg tea.KeyMsg) (keyPress, bool) {
	value := strings.TrimSpace(msg.String())
	if value == "" {
		return "", false
	}
	return keyPress(value), true
}

func formatKeySequence(sequence keySequence) string {
	if len(sequence) == 0 {
		return ""
	}
	parts := make([]string, 0, len(sequence))
	for _, press := range sequence {
		parts = append(parts, formatKeyPress(press))
	}
	return strings.Join(parts, " ")
}

func formatKeyPress(press keyPress) string {
	value := string(press)
	if strings.HasPrefix(value, "ctrl+") && len(value) > len("ctrl+") {
		return "C-" + value[len("ctrl+"):]
	}
	if strings.HasPrefix(value, "alt+") && len(value) > len("alt+") {
		return "M-" + value[len("alt+"):]
	}
	return value
}

func validateBindings(commands map[commandID]commandSpec, bindings []bindingSpec) error {
	seenExact := make(map[uiContext]map[string]commandID)
	seenPrefix := make(map[uiContext][]bindingSpec)
	canonicalByCommandCtx := make(map[commandID]map[uiContext]bool)
	for _, binding := range bindings {
		if _, ok := commands[binding.commandID]; !ok {
			return fmt.Errorf("binding references unknown command %q", binding.commandID)
		}
		if strings.TrimSpace(string(binding.context)) == "" {
			return fmt.Errorf("binding context is required for %q", binding.commandID)
		}
		if len(binding.sequence) == 0 {
			return fmt.Errorf("binding sequence is required for %q", binding.commandID)
		}
		for _, press := range binding.sequence {
			if strings.TrimSpace(string(press)) == "" {
				return fmt.Errorf("binding sequence contains empty key for %q", binding.commandID)
			}
		}
		if seenExact[binding.context] == nil {
			seenExact[binding.context] = make(map[string]commandID)
		}
		sequenceKey := keySequenceString(binding.sequence)
		if existing, ok := seenExact[binding.context][sequenceKey]; ok {
			return fmt.Errorf("duplicate binding %q in %q for %q and %q", formatKeySequence(binding.sequence), binding.context, existing, binding.commandID)
		}
		for _, existing := range seenPrefix[binding.context] {
			if keySequenceHasPrefix(binding.sequence, existing.sequence) || keySequenceHasPrefix(existing.sequence, binding.sequence) {
				return fmt.Errorf("ambiguous prefix overlap in %q between %q and %q", binding.context, formatKeySequence(existing.sequence), formatKeySequence(binding.sequence))
			}
		}
		seenExact[binding.context][sequenceKey] = binding.commandID
		seenPrefix[binding.context] = append(seenPrefix[binding.context], binding)
		if binding.canonical {
			if canonicalByCommandCtx[binding.commandID] == nil {
				canonicalByCommandCtx[binding.commandID] = make(map[uiContext]bool)
			}
			if canonicalByCommandCtx[binding.commandID][binding.context] {
				return fmt.Errorf("multiple canonical bindings for %q in %q", binding.commandID, binding.context)
			}
			canonicalByCommandCtx[binding.commandID][binding.context] = true
		}
	}
	return nil
}

func keySequenceHasPrefix(sequence keySequence, prefix keySequence) bool {
	if len(prefix) >= len(sequence) {
		return false
	}
	for index := range prefix {
		if sequence[index] != prefix[index] {
			return false
		}
	}
	return true
}

func keySequenceString(sequence keySequence) string {
	parts := make([]string, 0, len(sequence))
	for _, press := range sequence {
		parts = append(parts, string(press))
	}
	return strings.Join(parts, "\x00")
}

func alwaysVisible(commandContext) bool { return true }

func alwaysEnabled(commandContext) bool { return true }

func multipleTabsVisible(ctx commandContext) bool {
	return ctx.model != nil && len(ctx.model.tabs) > 1
}

func searchTabVisible(ctx commandContext) bool {
	return ctx.model != nil && ctx.model.activeTab == tabSearch
}

func consoleTabVisible(ctx commandContext) bool {
	return ctx.model != nil && ctx.model.activeTab == tabConsole
}

func hasSearchResults(ctx commandContext) bool {
	return searchTabVisible(ctx) && len(ctx.model.results) > 0
}

func runQuit(m *model) tea.Cmd {
	m.cancelInFlight()
	return tea.Quit
}

func runNextTab(m *model) tea.Cmd {
	m.switchTab(1)
	return nil
}

func runPreviousTab(m *model) tea.Cmd {
	m.switchTab(-1)
	return nil
}

func runSearchTab(m *model) tea.Cmd {
	m.setActiveTab(tabSearch)
	return nil
}

func runConsoleTab(m *model) tea.Cmd {
	m.setActiveTab(tabConsole)
	return nil
}

func runSubmitOrOpen(m *model) tea.Cmd { return m.submitOrOpen() }

func runMoveSelectionDown(m *model) tea.Cmd { return m.moveSelectionInPlace(1) }

func runMoveSelectionUp(m *model) tea.Cmd { return m.moveSelectionInPlace(-1) }

func runPageResultsDown(m *model) tea.Cmd { return m.moveSelectionInPlace(m.resultPageSize()) }

func runPageResultsUp(m *model) tea.Cmd { return m.moveSelectionInPlace(-m.resultPageSize()) }

func runTogglePreview(m *model) tea.Cmd { return m.togglePreview() }

func runConsolePageDown(m *model) tea.Cmd {
	m.console.pageDown(m.bodyHeight())
	return nil
}

func runConsolePageUp(m *model) tea.Cmd {
	m.console.pageUp(m.bodyHeight())
	return nil
}
