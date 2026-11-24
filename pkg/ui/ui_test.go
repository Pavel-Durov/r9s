package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pavel-durov/r9s/pkg/cloudrun"
)

// recorder is the demo backend, but records actions instead of running them.
type recorder struct {
	cloudrun.Fake
	ran chan string
}

func (r recorder) Run(a cloudrun.Action, res cloudrun.Resource) error {
	r.ran <- fmt.Sprint(a, " ", res.Name)
	return nil
}

// waitFor polls the simulated screen until it shows want.
func waitFor(t *testing.T, app *tview.Application, s tcell.SimulationScreen, want string) {
	t.Helper()
	var text string
	ok := assert.Eventually(t, func() bool {
		var b strings.Builder
		app.QueueUpdate(func() { // read on the event loop so it doesn't race with drawing
			cells, w, _ := s.GetContents()
			for i, c := range cells {
				if i%w == 0 {
					b.WriteByte('\n')
				}
				b.Write(c.Bytes)
			}
		})
		text = b.String()
		return strings.Contains(text, want)
	}, time.Second, 10*time.Millisecond)
	require.True(t, ok, "screen never showed %q:%s", want, text)
}

func TestPickProjectThenSwitchKinds(t *testing.T) {
	s := tcell.NewSimulationScreen("")
	require.NoError(t, s.Init())
	s.SetSize(120, 20)
	b := recorder{cloudrun.Demo, make(chan string, 1)}
	app := New(b, "").SetScreen(s)
	go app.Run()
	defer app.Stop()

	waitFor(t, app, s, "Demo Production")
	s.InjectKey(tcell.KeyEnter, 0, tcell.ModNone) // pick "demo-prod"
	waitFor(t, app, s, "worker-search-index")
	waitFor(t, app, s, "6 services")
	s.InjectKey(tcell.KeyDown, 0, tcell.ModNone)
	s.InjectKey(tcell.KeyCtrlR, 0, tcell.ModCtrl)
	select {
	case got := <-b.ran:
		assert.Equal(t, fmt.Sprint(cloudrun.Restart, " web"), got)
	case <-time.After(time.Second):
		t.Fatal("ctrl-r never reached the backend")
	}
	s.InjectKey(tcell.KeyRune, 'L', tcell.ModNone)
	select {
	case got := <-b.ran:
		assert.Equal(t, fmt.Sprint(cloudrun.Tail, " web"), got)
	case <-time.After(time.Second):
		t.Fatal("L never reached the backend")
	}
	s.InjectKey(tcell.KeyTab, 0, tcell.ModNone)
	waitFor(t, app, s, "nightly-etl-k2x9p")
}
