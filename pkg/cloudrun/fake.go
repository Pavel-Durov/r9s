package cloudrun

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Fake is an in-memory backend for running r9s without GCP (`r9s -mock`) and for tests.
// It holds the same JSON gcloud prints, so rows go through the real parser.
type Fake struct {
	ProjectList []string                   `json:"projects"`  // "id\tname" lines, like Gcloud.Projects
	Resources   map[string]json.RawMessage `json:"resources"` // "project/kind" -> `gcloud run <kind> list --format=json` output
}

func (f Fake) List(k *Kind, project string) ([]Resource, error) {
	out, ok := f.Resources[project+"/"+k.Name]
	if !ok {
		return nil, nil // gcloud prints [] for an empty list
	}
	return parse(k, project, out)
}

func (f Fake) Projects() ([]string, error) { return f.ProjectList, nil }

// Run acts on the demo data: config and logs are generated, edits are discarded, ssh and console are unavailable.
func (f Fake) Run(a Action, r Resource) error {
	switch a {
	case Describe:
		y, err := f.yaml(r)
		if err != nil {
			return err
		}
		page(strings.NewReader(y), false)
	case Logs:
		var b strings.Builder
		now := time.Now()
		for i := range 200 {
			b.WriteString(fakeLog(r.Name, now.Add(time.Duration(i-200)*7*time.Second), i) + "\n")
		}
		page(strings.NewReader(b.String()), true)
	case Tail:
		fmt.Println("streaming fake logs for " + r.Name + " (ctrl-c to stop)...")
		stream(time.Second, func(i int, t time.Time) string { return fakeLog(r.Name, t, i) })
	case Edit:
		y, err := f.yaml(r)
		if err != nil {
			return err
		}
		path, changed, err := editTemp(r.Name, []byte(y))
		os.Remove(path)
		if err != nil {
			return err
		}
		if changed {
			fmt.Println("demo mode: changes not applied")
		} else {
			fmt.Println("no changes")
		}
		pause()
	default:
		return errors.New("not available in demo mode")
	}
	return nil
}

var fakeMessages = []string{
	"INFO  GET /healthz 200 2ms",
	"INFO  processed batch size=50 took=840ms",
	"INFO  POST /v1/tasks 201 34ms",
	"WARN  slow upstream call took=1.2s",
	"INFO  GET /v1/tasks?limit=20 200 12ms",
	"ERROR upstream timeout, retrying attempt=2",
}

// fakeLog is the i-th fake log line for name at t.
func fakeLog(name string, t time.Time, i int) string {
	return fmt.Sprintf("%s  %s  %s", t.UTC().Format("2006-01-02 15:04:05"), name, fakeMessages[i%len(fakeMessages)])
}

// yaml renders r's demo.json entry like `gcloud run <kind> describe --format=yaml`.
func (f Fake) yaml(r Resource) (string, error) {
	var rs []map[string]any
	if err := json.Unmarshal(f.Resources[r.Project+"/"+r.Kind.Name], &rs); err != nil {
		return "", err
	}
	for _, m := range rs {
		if md, _ := m["metadata"].(map[string]any); md["name"] == r.Name {
			var b strings.Builder
			writeYAML(&b, m, "")
			return b.String(), nil
		}
	}
	return "", fmt.Errorf("%s not found in demo.json", r.Name)
}

// writeYAML renders decoded JSON as block YAML with sorted keys, kubectl style.
func writeYAML(b *strings.Builder, v any, indent string) {
	switch v := v.(type) {
	case map[string]any:
		for _, k := range slices.Sorted(maps.Keys(v)) {
			b.WriteString(indent + k + ":")
			writeYAMLChild(b, v[k], indent)
		}
	case []any:
		for _, e := range v {
			if m, ok := e.(map[string]any); ok && len(m) > 0 {
				var sub strings.Builder
				writeYAML(&sub, m, indent+"  ")
				b.WriteString(indent + "- " + strings.TrimPrefix(sub.String(), indent+"  ")) // first key on the dash line
				continue
			}
			b.WriteString(indent + "-")
			writeYAMLChild(b, e, indent)
		}
	}
}

func writeYAMLChild(b *strings.Builder, v any, indent string) {
	switch c := v.(type) {
	case map[string]any:
		if len(c) == 0 {
			b.WriteString(" {}\n")
			return
		}
		b.WriteString("\n")
		writeYAML(b, c, indent+"  ")
	case []any:
		if len(c) == 0 {
			b.WriteString(" []\n")
			return
		}
		b.WriteString("\n")
		writeYAML(b, c, indent)
	case string:
		_, numErr := strconv.ParseFloat(c, 64)
		if c == "" || numErr == nil || strings.EqualFold(c, "true") || strings.EqualFold(c, "false") || strings.ContainsAny(c, ":#{}[],&*!|>'\"%@`") {
			c = strconv.Quote(c) // would otherwise parse as another type or break YAML syntax
		}
		b.WriteString(" " + c + "\n")
	default: // numbers, bools, null
		j, _ := json.Marshal(c)
		b.WriteString(" " + string(j) + "\n")
	}
}

//go:embed demo.json
var demoJSON []byte

// Demo is the sample data `r9s -mock` shows; edit demo.json to change it.
var Demo = func() Fake {
	var f Fake
	if err := json.Unmarshal(demoJSON, &f); err != nil {
		panic("demo.json: " + err.Error())
	}
	return f
}()
