// Package ui is the k9s-style terminal UI.
package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/pavel-durov/r9s/pkg/cloudrun"
)

const hint = "[yellow]<?>[-] help  [yellow]<q>[-] quit"

const keys = `
 p        pick project
 tab      switch services / jobs
 enter,d  describe
 e        edit YAML
 l        logs (last 1000 lines)
 L        live tail logs (ctrl-c to stop)
 s        ssh
 ctrl-r   restart service (new revision, after y/N)
 o        open in GCP console
 /        filter by name
 esc      back (clear filter, then project picker)
 r        refresh
 q        quit

 press any key to close`

// Backend is where projects, resources and actions come from: cloudrun.Gcloud for real, cloudrun.Fake for -mock and tests.
type Backend interface {
	Projects() ([]string, error) // "id\tname" lines
	List(k *cloudrun.Kind, project string) ([]cloudrun.Resource, error)
	// Run performs a on r. Every action but Console takes over the terminal, so the UI suspends around it.
	Run(a cloudrun.Action, r cloudrun.Resource) error
}

// actions maps keys to what they do to the selected resource; enter is also Describe.
var actions = map[rune]cloudrun.Action{
	'd': cloudrun.Describe, 'l': cloudrun.Logs, 'L': cloudrun.Tail,
	's': cloudrun.SSH, 'e': cloudrun.Edit, 'o': cloudrun.Console,
}

// Run starts the TUI. With an empty project it opens the project picker first.
func Run(b Backend, project string) error {
	return New(b, project).Run()
}

// New builds the TUI without running it, so tests can attach a simulation screen.
func New(backend Backend, project string) *tview.Application {
	app := tview.NewApplication()
	header := tview.NewTextView().SetDynamicColors(true)
	table := tview.NewTable().SetSelectable(true, false).SetFixed(1, 0).
		SetSelectedStyle(tcell.StyleDefault.Background(tcell.ColorAqua).Foreground(tcell.ColorBlack))
	footer := tview.NewTextView().SetDynamicColors(true)
	filterInput := tview.NewInputField().SetLabel("/")
	bottom := tview.NewPages().AddPage("footer", footer, true, true).AddPage("filter", filterInput, true, false)
	root := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(header, 1, 0, false).
		AddItem(table, 0, 1, true).
		AddItem(bottom, 1, 0, false)

	pickerFilter := tview.NewInputField().SetLabel("filter: ")
	pickerList := tview.NewList().ShowSecondaryText(false).SetHighlightFullLine(true)
	picker := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(pickerFilter, 1, 0, true).
		AddItem(pickerList, 0, 1, false)
	picker.SetBorder(true).SetTitle(" select project (<enter> pick, </> filter, <esc> back) ")
	pages := tview.NewPages().AddPage("main", root, true, true).AddPage("picker", picker, true, false)
	helpView := tview.NewTextView().SetText(keys)
	helpView.SetBorder(true).SetTitle(" key bindings ")
	helpView.SetInputCapture(func(*tcell.EventKey) *tcell.EventKey {
		pages.HidePage("help")
		app.SetFocus(table)
		return nil
	})
	pages.AddPage("help", helpView, true, false)

	st := newState(project)
	var visible []cloudrun.Resource // rows on screen, in table order

	render := func(status string) {
		header.SetText(fmt.Sprintf("[::b]r9s[::-]  project: [green]%s[-]  view: [green]%s[-]  %s", st.project, st.kind.Name, hint))
		table.Clear()
		for i, c := range st.kind.Columns {
			table.SetCell(0, i, tview.NewTableCell(c).SetAttributes(tcell.AttrBold).SetSelectable(false).SetExpansion(1))
		}
		visible = st.visible()
		for i, r := range visible {
			color := tcell.ColorDefault
			if !r.Ready {
				color = tcell.ColorRed
			}
			for j, v := range r.Cells {
				table.SetCell(i+1, j, tview.NewTableCell(v).SetTextColor(color).SetExpansion(1))
			}
		}
		if status == "" {
			status = st.status()
		}
		footer.SetText(status)
	}

	load := func() {
		k, p := st.kind, st.project
		render("loading " + k.Name + "...")
		go func() {
			rs, err := backend.List(k, p)
			app.QueueUpdateDraw(func() {
				if !st.store(p, k, rs) {
					return
				}
				if err != nil {
					render("[red]error: " + err.Error())
				} else {
					render("")
				}
			})
		}()
	}

	var projects []string
	fillPicker := func() {
		pickerList.Clear()
		f := strings.ToLower(pickerFilter.GetText())
		for _, p := range projects {
			if strings.Contains(strings.ToLower(p), f) {
				id := strings.SplitN(p, "\t", 2)[0]
				pickerList.AddItem(strings.ReplaceAll(p, "\t", "  [gray]")+"[-]", "", 0, func() {
					st.pick(id)
					pages.HidePage("picker")
					app.SetFocus(table)
					load()
				})
			}
		}
	}
	var closeFilter func()
	showPicker := func() {
		closeFilter()
		pages.ShowPage("picker")
		app.SetFocus(pickerList)
		if projects != nil {
			fillPicker()
			return
		}
		pickerList.Clear().AddItem("loading projects...", "", 0, nil)
		go func() {
			ps, err := backend.Projects()
			app.QueueUpdateDraw(func() {
				if err != nil {
					pickerList.Clear().AddItem("[red]"+err.Error(), "", 0, nil)
					return
				}
				projects = ps
				fillPicker()
			})
		}()
	}
	closeFilter = func() {
		pickerFilter.SetText("")
		picker.ResizeItem(pickerFilter, 0, 0) // hidden until <'/'>
		app.SetFocus(pickerList)
	}
	pickerFilter.SetChangedFunc(func(string) { fillPicker() })
	pickerFilter.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyUp, tcell.KeyDown, tcell.KeyPgUp, tcell.KeyPgDn, tcell.KeyEnter:
			pickerList.InputHandler()(ev, nil) // navigate/select the list while typing
			return nil
		case tcell.KeyEscape:
			closeFilter()
			return nil
		}
		return ev
	})
	pickerList.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch {
		case ev.Rune() == '/':
			picker.ResizeItem(pickerFilter, 1, 0)
			app.SetFocus(pickerFilter)
			return nil
		case ev.Key() == tcell.KeyEscape:
			if st.project != "" {
				pages.HidePage("picker")
				app.SetFocus(table)
			}
			return nil
		}
		return ev
	})

	filterInput.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			st.filter = filterInput.GetText()
		}
		bottom.SwitchToPage("footer")
		app.SetFocus(table)
		render("")
	})

	act := func(a cloudrun.Action, r cloudrun.Resource) {
		var err error
		if a == cloudrun.Console {
			err = backend.Run(a, r)
		} else {
			app.Suspend(func() { err = backend.Run(a, r) })
		}
		switch {
		case err != nil:
			render("[red]" + err.Error())
		case a == cloudrun.Edit, a == cloudrun.Restart:
			load()
		}
	}

	table.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		sel, _ := table.GetSelection()
		switch {
		case ev.Key() == tcell.KeyTab:
			needsLoad := st.nextKind()
			table.Select(1, 0)
			if needsLoad {
				load()
			} else {
				render("")
			}
		case ev.Key() == tcell.KeyEscape: // back: clear filter first, then up to the project picker
			if st.filter != "" {
				st.filter = ""
				render("")
			} else {
				showPicker()
			}
		case ev.Rune() == '/':
			filterInput.SetText(st.filter)
			bottom.SwitchToPage("filter")
			app.SetFocus(filterInput)
		case ev.Rune() == '?':
			pages.ShowPage("help")
			app.SetFocus(helpView)
		case ev.Rune() == 'p':
			showPicker()
		case ev.Key() == tcell.KeyRune && ev.Rune() == 'r': // ctrl-r also carries rune 'r'
			load()
		case ev.Rune() == 'q':
			app.Stop()
		default:
			a, ok := actions[ev.Rune()]
			switch ev.Key() {
			case tcell.KeyEnter:
				a, ok = cloudrun.Describe, true
			case tcell.KeyCtrlR:
				a, ok = cloudrun.Restart, true
			}
			if !ok {
				return ev // j/k/arrows etc. handled by the table
			}
			if sel >= 1 && sel <= len(visible) {
				act(a, visible[sel-1])
			}
		}
		return nil
	})

	if project == "" {
		showPicker()
	} else {
		load()
	}
	return app.SetRoot(pages, true)
}
