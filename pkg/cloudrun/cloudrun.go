// Package cloudrun lists Cloud Run resources and builds the gcloud commands r9s runs on them.
package cloudrun

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

var Kinds = []string{"services", "jobs"}

var Columns = map[string][]string{
	"services": {"NAME", "REGION", "READY", "SCALE", "MEM", "REVISION", "URL", "AGE"},
	"jobs":     {"NAME", "REGION", "READY", "LAST EXECUTION", "COMPLETED"},
}

// resource is the subset of the Knative-style JSON that `gcloud run ... list` returns.
type resource struct {
	Metadata struct {
		Name   string            `json:"name"`
		Labels map[string]string `json:"labels"`
	} `json:"metadata"`
	Spec struct {
		Template struct {
			Metadata struct {
				Annotations map[string]string `json:"annotations"`
			} `json:"metadata"`
			Spec struct {
				Containers []struct {
					Resources struct {
						Limits map[string]string `json:"limits"`
					} `json:"resources"`
				} `json:"containers"`
			} `json:"spec"`
		} `json:"template"`
	} `json:"spec"`
	Status struct {
		URL                     string `json:"url"`
		LatestReadyRevisionName string `json:"latestReadyRevisionName"`
		LatestCreatedExecution  struct {
			Name                string `json:"name"`
			CompletionTimestamp string `json:"completionTimestamp"`
		} `json:"latestCreatedExecution"`
		Conditions []struct {
			Type               string `json:"type"`
			Status             string `json:"status"`
			LastTransitionTime string `json:"lastTransitionTime"`
		} `json:"conditions"`
	} `json:"status"`
}

// age renders an RFC3339 timestamp as a compact age like "3d4h"; "" if unparsable.
func age(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ""
	}
	h, m := int(time.Since(t).Hours()), int(time.Since(t).Minutes())%60
	if h < 24 {
		return fmt.Sprintf("%dh%dm", h, m)
	}
	return fmt.Sprintf("%dd%dh", h/24, h%24)
}

// mem returns the first container's memory limit in MB ("512Mi" -> "512MB", "1Gi" -> "1024MB").
func mem(r resource) string {
	cs := r.Spec.Template.Spec.Containers
	if len(cs) == 0 {
		return "-"
	}
	l := cs[0].Resources.Limits["memory"]
	if n, ok := strings.CutSuffix(l, "Gi"); ok {
		if v, err := strconv.Atoi(n); err == nil {
			return fmt.Sprintf("%dMB", v*1024)
		}
	}
	if n, ok := strings.CutSuffix(l, "Mi"); ok {
		return n + "MB"
	}
	return orDash(l)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func row(kind string, r resource) []string {
	ready, deployed := "?", ""
	for _, c := range r.Status.Conditions {
		if c.Type == "Ready" {
			ready, deployed = c.Status, c.LastTransitionTime // same source as gcloud's "LAST DEPLOYED AT"
		}
	}
	region := r.Metadata.Labels["cloud.googleapis.com/location"]
	if kind == "services" {
		a := r.Spec.Template.Metadata.Annotations
		scale := orDash(a["autoscaling.knative.dev/minScale"]) + "/" + orDash(a["autoscaling.knative.dev/maxScale"]) // min/max instances; live count isn't in gcloud's JSON
		return []string{r.Metadata.Name, region, ready, scale, mem(r), r.Status.LatestReadyRevisionName, r.Status.URL, age(deployed)}
	}
	ex := r.Status.LatestCreatedExecution
	return []string{r.Metadata.Name, region, ready, ex.Name, ex.CompletionTimestamp}
}

func gcloud(project string, args ...string) []string {
	cmd := append([]string{"gcloud"}, args...)
	if project != "" {
		cmd = append(cmd, "--project", project)
	}
	return cmd
}

func Fetch(kind, project string) ([][]string, error) {
	c := gcloud(project, "run", kind, "list", "--format=json")
	out, err := exec.Command(c[0], c[1:]...).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("%s", gcloudError(string(ee.Stderr)))
		}
		return nil, err
	}
	var rs []resource
	if err := json.Unmarshal(out, &rs); err != nil {
		return nil, err
	}
	rows := make([][]string, len(rs))
	for i, r := range rs {
		rows[i] = row(kind, r)
	}
	return rows, nil
}

// gcloudError picks the "ERROR: (gcloud.x) msg" line out of stderr and trims it to fit one line.
func gcloudError(stderr string) string {
	for _, l := range strings.Split(stderr, "\n") {
		if _, msg, ok := strings.Cut(l, "ERROR: ("); ok {
			_, msg, _ = strings.Cut(msg, ") ")
			msg, _, _ = strings.Cut(msg, " This command is authenticated")
			return msg
		}
	}
	return strings.TrimSpace(stderr)
}

// FetchProjects returns "id\tname" lines for every project the user can see.
func FetchProjects() ([]string, error) {
	out, err := exec.Command("gcloud", "projects", "list", "--sort-by=projectId", "--format=value(projectId,name)").Output()
	if err != nil {
		return nil, fmt.Errorf("gcloud projects list failed: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n"), nil
}

// Command returns the gcloud command for a key on the selected row, and whether to page its output.
func Command(key rune, kind, name, region, project string) ([]string, bool) {
	switch key {
	case 'd':
		return gcloud(project, "run", kind, "describe", name, "--region", region), true
	case 'l':
		return gcloud(project, "run", kind, "logs", "read", name, "--region", region, "--limit", "1000"), true
	case 'L':
		// Streams until ctrl-c; needs `gcloud components install beta log-streaming`.
		return gcloud(project, "beta", "run", kind, "logs", "tail", name, "--region", region), false
	default: // 's'
		// Hidden gcloud commands; need `gcloud run deploy --ssh`. Jobs ssh is alpha-only.
		track := "beta"
		if kind == "jobs" {
			track = "alpha"
		}
		return gcloud(project, track, "run", kind, "ssh", name, "--region", region), false
	}
}

// Edit exports the resource YAML, opens $EDITOR on it, and on confirmation applies it with `gcloud run <kind> replace`.
func Edit(kind, name, region, project string) {
	defer pause()
	f, err := os.CreateTemp("", name+"-*.yaml")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer os.Remove(f.Name())
	export := gcloud(project, "run", kind, "describe", name, "--region", region, "--format=export")
	out, err := exec.Command(export[0], export[1:]...).CombinedOutput()
	if err != nil {
		fmt.Print(string(out))
		return
	}
	f.Write(out)
	f.Close()
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	ed := exec.Command("sh", "-c", editor+` "$0"`, f.Name()) // sh so EDITOR="code -w" works
	ed.Stdin, ed.Stdout, ed.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := ed.Run(); err != nil {
		fmt.Println(err)
		return
	}
	edited, _ := os.ReadFile(f.Name())
	if string(edited) == string(out) {
		fmt.Println("no changes")
		return
	}
	fmt.Printf("apply changes to %s %s? [y/N] ", kind, name)
	var yes string
	if fmt.Scanln(&yes); yes != "y" && yes != "Y" {
		fmt.Println("aborted")
		return
	}
	c := gcloud(project, "run", kind, "replace", f.Name(), "--region", region)
	fmt.Println("$ " + strings.Join(c, " "))
	run := exec.Command(c[0], c[1:]...)
	run.Stdin, run.Stdout, run.Stderr = os.Stdin, os.Stdout, os.Stderr
	_ = run.Run()
}

// OpenConsole opens the resource's page in the GCP console in the default browser.
func OpenConsole(kind, name, region, project string) error {
	path := "detail"
	if kind == "jobs" {
		path = "jobs/details"
	}
	url := fmt.Sprintf("https://console.cloud.google.com/run/%s/%s/%s?project=%s", path, region, name, project)
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	return exec.Command(opener, url).Start()
}

// Exec runs cmd in the foreground, piping its output to less when paged.
func Exec(cmd []string, paged bool) {
	fmt.Println("$ " + strings.Join(cmd, " "))
	// Catch ctrl-c so it stops the child (e.g. a log tail) instead of killing r9s.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)
	c := exec.Command(cmd[0], cmd[1:]...)
	if paged {
		less := exec.Command("less", "-R", "+G")
		less.Stdout, less.Stderr = os.Stdout, os.Stderr
		less.Stdin, _ = c.StdoutPipe()
		c.Stderr = c.Stdout // show gcloud errors in the pager too
		if err := c.Start(); err == nil {
			_ = less.Run()
			_ = c.Wait()
			return
		}
	}
	if slices.Contains(cmd, "tail") {
		fmt.Println("waiting for new log lines (ctrl-c to stop)...")
	}
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	_ = c.Run()
	pause()
}

func pause() {
	fmt.Print("\n[press enter to return]")
	fmt.Scanln()
}
