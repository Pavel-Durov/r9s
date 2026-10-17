package cloudrun

// Terminal helpers for actions that take over the screen while the TUI is suspended.

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"time"
)

// page shows r in less, opened at the end when fromEnd is set.
func page(r io.Reader, fromEnd bool) {
	args := []string{"-R"}
	if fromEnd {
		args = append(args, "+G")
	}
	less := exec.Command("less", args...)
	less.Stdin, less.Stdout, less.Stderr = r, os.Stdout, os.Stderr
	_ = less.Run()
}

// runPaged runs cmd and pages its stdout and stderr together.
func runPaged(cmd []string, fromEnd bool) {
	fmt.Println("$ " + strings.Join(cmd, " "))
	_, stop := interrupts()
	defer stop()
	c := exec.Command(cmd[0], cmd[1:]...)
	out, err := c.StdoutPipe()
	if err == nil {
		c.Stderr = c.Stdout // show gcloud errors in the pager too
		err = c.Start()
	}
	if err != nil {
		fmt.Println(err)
		pause()
		return
	}
	page(out, fromEnd)
	_ = c.Wait()
}

// runForeground runs cmd attached to the terminal, then waits for enter.
func runForeground(cmd []string) {
	fmt.Println("$ " + strings.Join(cmd, " "))
	_, stop := interrupts()
	defer stop()
	_ = attached(cmd).Run()
	pause()
}

// stream prints line(i, t) every interval until ctrl-c, then waits for enter.
func stream(every time.Duration, line func(i int, t time.Time) string) {
	sig, stop := interrupts()
	defer stop()
	tick := time.NewTicker(every)
	defer tick.Stop()
	for i := 0; ; i++ {
		select {
		case <-sig:
			pause()
			return
		case t := <-tick.C:
			fmt.Println(line(i, t))
		}
	}
}

// interrupts catches ctrl-c so it stops the foreground child or stream instead of killing r9s.
// Call stop to restore the default.
func interrupts() (sig <-chan os.Signal, stop func()) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	return c, func() { signal.Stop(c) }
}

// editTemp opens content in $EDITOR (vi by default) as a temp YAML file named after name.
// It returns the file's path, which the caller removes, and whether the file was changed.
func editTemp(name string, content []byte) (path string, changed bool, err error) {
	f, err := os.CreateTemp("", name+"-*.yaml")
	if err != nil {
		return "", false, err
	}
	_, err = f.Write(content)
	f.Close()
	if err != nil {
		return f.Name(), false, err
	}
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	if err := attached([]string{"sh", "-c", editor + ` "$0"`, f.Name()}).Run(); err != nil { // sh so EDITOR="code -w" works
		return f.Name(), false, err
	}
	edited, err := os.ReadFile(f.Name())
	return f.Name(), string(edited) != string(content), err
}

func confirm(prompt string) bool {
	fmt.Print(prompt + " [y/N] ")
	var yes string
	fmt.Scanln(&yes)
	return yes == "y" || yes == "Y"
}

func attached(cmd []string) *exec.Cmd {
	c := exec.Command(cmd[0], cmd[1:]...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c
}

// openURL opens url in the default browser without waiting for it.
func openURL(url string) error {
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	return exec.Command(opener, url).Start()
}

func pause() {
	fmt.Print("\n[press enter to return]")
	fmt.Scanln()
}
