// Package ui is the k9s-style terminal UI.
package ui

import (
	"fmt"
	"slices"
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
 l        logs
 s        ssh
 /        filter by name
 esc      back (clear filter, then project picker)
 r        refresh
 q        quit

 press any key to close`

// Run starts the TUI. With an empty project it opens the project picker first.
func Run(project string) error {
	app := tview.NewApplication()
	header := tview.NewTextView().SetDynamicColors(true)
	table := tview.NewTable().SetSelectable(true, false).SetFixed(1, 0)
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

	kind, filter := "services", ""
	cache := map[string][][]string{}
	var visible [][]string

	render := func(status string) {
		header.SetText(fmt.Sprintf("[::b]r9s[::-]  project: [green]%s[-]  view: [green]%s[-]  %s", project, kind, hint))
		table.Clear()
		for i, c := range cloudrun.Columns[kind] {
			table.SetCell(0, i, tview.NewTableCell(c).SetAttributes(tcell.AttrBold).SetSelectable(false).SetExpansion(1))
		}
		visible = nil
		for _, r := range cache[kind] {
			if filter == "" || strings.Contains(strings.ToLower(r[0]), strings.ToLower(filter)) {
				visible = append(visible, r)
			}
		}
		for i, r := range visible {
			color := tcell.ColorDefault
			if r[2] != "True" {
				color = tcell.ColorRed
			}
			for j, v := range r {
				table.SetCell(i+1, j, tview.NewTableCell(v).SetTextColor(color).SetExpansion(1))
			}
		}
		if status == "" {
			status = fmt.Sprintf("%d %s", len(visible), kind)
			if filter != "" {
				status += fmt.Sprintf("  (filter: %s, <esc> clears)", filter)
			}
		}
		footer.SetText(status)
	}

	load := func() {
		k, p := kind, project
		render("loading " + k + "...")
		go func() {
			rows, err := cloudrun.Fetch(k, p)
			app.QueueUpdateDraw(func() {
				if p != project { // project switched while loading
					return
				}
				cache[k] = rows
				if k != kind {
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
					project, kind, filter = id, "services", ""
					cache = map[string][][]string{}
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
			ps, err := cloudrun.FetchProjects()
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
			if project != "" {
				pages.HidePage("picker")
				app.SetFocus(table)
			}
			return nil
		}
		return ev
	})

	filterInput.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			filter = filterInput.GetText()
		}
		bottom.SwitchToPage("footer")
		app.SetFocus(table)
		render("")
	})

	table.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		sel, _ := table.GetSelection()
		switch {
		case ev.Key() == tcell.KeyTab:
			kind = cloudrun.Kinds[(slices.Index(cloudrun.Kinds, kind)+1)%len(cloudrun.Kinds)]
			filter = ""
			table.Select(1, 0)
			if _, ok := cache[kind]; ok {
				render("")
			} else {
				load()
			}
		case ev.Key() == tcell.KeyEscape: // back: clear filter first, then up to the project picker
			if filter != "" {
				filter = ""
				render("")
			} else {
				showPicker()
			}
		case ev.Key() == tcell.KeyEnter || ev.Rune() == 'd' || ev.Rune() == 'l' || ev.Rune() == 's':
			if sel < 1 || sel > len(visible) {
				return nil
			}
			key := ev.Rune()
			if ev.Key() == tcell.KeyEnter {
				key = 'd'
			}
			r := visible[sel-1]
			cmd, paged := cloudrun.Command(key, kind, r[0], r[1], project)
			app.Suspend(func() { cloudrun.Exec(cmd, paged) })
		case ev.Rune() == 'e':
			if sel >= 1 && sel <= len(visible) {
				r := visible[sel-1]
				app.Suspend(func() { cloudrun.Edit(kind, r[0], r[1], project) })
				load()
			}
		case ev.Rune() == '/':
			filterInput.SetText(filter)
			bottom.SwitchToPage("filter")
			app.SetFocus(filterInput)
		case ev.Rune() == '?':
			pages.ShowPage("help")
			app.SetFocus(helpView)
		case ev.Rune() == 'p':
			showPicker()
		case ev.Rune() == 'r':
			load()
		case ev.Rune() == 'q':
			app.Stop()
		default:
			return ev // j/k/arrows etc. handled by the table
		}
		return nil
	})

	if project == "" {
		showPicker()
	} else {
		load()
	}
	return app.SetRoot(pages, true).Run()
}
