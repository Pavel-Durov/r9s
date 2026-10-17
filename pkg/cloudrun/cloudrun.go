// Package cloudrun lists Cloud Run resources and runs actions on them, through gcloud or the demo Fake.
package cloudrun

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Kind is a Cloud Run resource type and everything that differs between services and jobs.
type Kind struct {
	Name        string // gcloud noun: "services" or "jobs"
	Columns     []string
	cells       func(resource) []string // one value per column
	consolePath string                  // console URL segment before /region/name
	sshTrack    string                  // gcloud release track that has `run <kind> ssh`
}

var (
	Services = &Kind{
		Name:        "services",
		Columns:     []string{"NAME", "REGION", "READY", "SCALE", "MEM", "REVISION", "URL", "AGE"},
		cells:       serviceCells,
		consolePath: "detail",
		sshTrack:    "beta", // hidden command; needs `gcloud run deploy --ssh`
	}
	Jobs = &Kind{
		Name:        "jobs",
		Columns:     []string{"NAME", "REGION", "READY", "LAST EXECUTION", "COMPLETED"},
		cells:       jobCells,
		consolePath: "jobs/details",
		sshTrack:    "alpha", // jobs ssh is alpha-only
	}
	Kinds = []*Kind{Services, Jobs}
)

// Resource is one service or job: named identity for actions, Cells for display.
type Resource struct {
	Kind                  *Kind
	Project, Name, Region string
	Ready                 bool
	Cells                 []string // one per Kind.Columns
}

// Action is something the user can do to a selected Resource.
type Action int

const (
	Describe Action = iota
	Logs            // last 1000 lines in a pager
	Tail            // stream new lines until ctrl-c
	SSH
	Edit    // edit the YAML in $EDITOR and apply it
	Console // open the GCP console in the browser; the only action that doesn't take over the terminal
)

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

// ready returns the Ready condition's status ("?" if missing) and when it last changed.
func ready(r resource) (status, since string) {
	status = "?"
	for _, c := range r.Status.Conditions {
		if c.Type == "Ready" {
			status, since = c.Status, c.LastTransitionTime // same source as gcloud's "LAST DEPLOYED AT"
		}
	}
	return status, since
}

func region(r resource) string { return r.Metadata.Labels["cloud.googleapis.com/location"] }

func serviceCells(r resource) []string {
	status, deployed := ready(r)
	a := r.Spec.Template.Metadata.Annotations
	scale := orDash(a["autoscaling.knative.dev/minScale"]) + "/" + orDash(a["autoscaling.knative.dev/maxScale"]) // min/max instances; live count isn't in gcloud's JSON
	return []string{r.Metadata.Name, region(r), status, scale, mem(r), r.Status.LatestReadyRevisionName, r.Status.URL, age(deployed)}
}

func jobCells(r resource) []string {
	status, _ := ready(r)
	ex := r.Status.LatestCreatedExecution
	return []string{r.Metadata.Name, region(r), status, ex.Name, ex.CompletionTimestamp}
}

// parse turns `gcloud run <kind> list --format=json` output into resources.
func parse(k *Kind, project string, out []byte) ([]Resource, error) {
	var rs []resource
	if err := json.Unmarshal(out, &rs); err != nil {
		return nil, err
	}
	res := make([]Resource, len(rs))
	for i, r := range rs {
		status, _ := ready(r)
		res[i] = Resource{Kind: k, Project: project, Name: r.Metadata.Name, Region: region(r), Ready: status == "True", Cells: k.cells(r)}
	}
	return res, nil
}

func gcloud(project string, args ...string) []string {
	cmd := append([]string{"gcloud"}, args...)
	if project != "" {
		cmd = append(cmd, "--project", project)
	}
	return cmd
}

// command is the gcloud command line for every action except Console.
func command(a Action, r Resource) []string {
	run := []string{"run", r.Kind.Name}
	var args []string
	switch a {
	case Describe:
		args = append(run, "describe", r.Name)
	case Logs:
		args = append(run, "logs", "read", r.Name, "--limit", "1000")
	case Tail: // needs `gcloud components install beta log-streaming`
		args = append(append([]string{"beta"}, run...), "logs", "tail", r.Name)
	case SSH:
		args = append(append([]string{r.Kind.sshTrack}, run...), "ssh", r.Name)
	case Edit:
		args = append(run, "describe", r.Name, "--format=export")
	}
	return gcloud(r.Project, append(args, "--region", r.Region)...)
}

func consoleURL(r Resource) string {
	return fmt.Sprintf("https://console.cloud.google.com/run/%s/%s/%s?project=%s", r.Kind.consolePath, r.Region, r.Name, r.Project)
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

// Gcloud is the real backend: it shells out to gcloud.
type Gcloud struct{}

// Projects returns "id\tname" lines for every project the user can see.
func (Gcloud) Projects() ([]string, error) {
	out, err := exec.Command("gcloud", "projects", "list", "--sort-by=projectId", "--format=value(projectId,name)").Output()
	if err != nil {
		return nil, fmt.Errorf("gcloud projects list failed: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n"), nil
}

func (Gcloud) List(k *Kind, project string) ([]Resource, error) {
	c := gcloud(project, "run", k.Name, "list", "--format=json")
	out, err := exec.Command(c[0], c[1:]...).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("%s", gcloudError(string(ee.Stderr)))
		}
		return nil, err
	}
	return parse(k, project, out)
}

func (Gcloud) Run(a Action, r Resource) error {
	switch a {
	case Console:
		return openURL(consoleURL(r))
	case Edit:
		edit(r)
	case Describe:
		runPaged(command(a, r), false)
	case Logs:
		runPaged(command(a, r), true)
	case Tail:
		fmt.Println("waiting for new log lines (ctrl-c to stop)...")
		runForeground(command(a, r))
	case SSH:
		runForeground(command(a, r))
	}
	return nil
}

// edit exports the resource YAML, opens $EDITOR on it, and on confirmation applies it with `gcloud run <kind> replace`.
func edit(r Resource) {
	defer pause()
	c := command(Edit, r)
	out, err := exec.Command(c[0], c[1:]...).CombinedOutput()
	if err != nil {
		fmt.Print(string(out))
		return
	}
	path, changed, err := editTemp(r.Name, out)
	defer os.Remove(path)
	switch {
	case err != nil:
		fmt.Println(err)
	case !changed:
		fmt.Println("no changes")
	case !confirm(fmt.Sprintf("apply changes to %s %s?", r.Kind.Name, r.Name)):
		fmt.Println("aborted")
	default:
		c = gcloud(r.Project, "run", r.Kind.Name, "replace", path, "--region", r.Region)
		fmt.Println("$ " + strings.Join(c, " "))
		_ = attached(c).Run()
	}
}
