package cloudrun

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	rs, err := parse(Services, "p", []byte(`[{"metadata":{"name":"api","labels":{"cloud.googleapis.com/location":"europe-west1"}},
		"spec":{"template":{"spec":{"containers":[{"resources":{"limits":{"memory":"1Gi"}}}]}}},
		"status":{"url":"https://api.run.app","latestReadyRevisionName":"api-001","conditions":[{"type":"Ready","status":"True"}]}},
		{"metadata":{"name":"etl"}}]`))
	require.NoError(t, err)
	require.Len(t, rs, 2)
	assert.Equal(t, Resource{Kind: Services, Project: "p", Name: "api", Region: "europe-west1", Ready: true,
		Cells: []string{"api", "europe-west1", "True", "-/-", "1024MB", "api-001", "https://api.run.app", ""}}, rs[0])
	assert.False(t, rs[1].Ready)
	assert.Equal(t, "?", rs[1].Cells[2])
}

func TestCommand(t *testing.T) {
	etl := Resource{Kind: Jobs, Project: "p", Name: "etl", Region: "us-central1"}
	assert.Equal(t, []string{"gcloud", "alpha", "run", "jobs", "ssh", "etl", "--region", "us-central1", "--project", "p"}, command(SSH, etl))
	assert.Equal(t, "https://console.cloud.google.com/run/jobs/details/us-central1/etl?project=p", consoleURL(etl))

	api := Resource{Kind: Services, Name: "api", Region: "eu"}
	assert.Equal(t, []string{"gcloud", "run", "services", "logs", "read", "api", "--limit", "1000", "--region", "eu"}, command(Logs, api))
	assert.Equal(t, []string{"gcloud", "beta", "run", "services", "logs", "tail", "api", "--region", "eu"}, command(Tail, api))
	assert.Equal(t, []string{"gcloud", "beta", "run", "services", "ssh", "api", "--region", "eu"}, command(SSH, api))
}

func TestAge(t *testing.T) {
	assert.Equal(t, "2d2h", age(time.Now().Add(-50*time.Hour).Format(time.RFC3339)))
}

func TestGcloudError(t *testing.T) {
	stderr := "API not enabled. Enable (y/N)?\nERROR: (gcloud.run.services.list) PERMISSION_DENIED: denied on 'namespaces/p/services'. This command is authenticated as a@b.c\nreason: SERVICE_DISABLED\n"
	assert.Equal(t, "PERMISSION_DENIED: denied on 'namespaces/p/services'.", gcloudError(stderr))
}

func TestRowMatchesColumns(t *testing.T) {
	var r resource
	require.NoError(t, json.Unmarshal([]byte(`{"metadata":{"name":"api"},
		"spec":{"template":{"metadata":{"annotations":{"autoscaling.knative.dev/minScale":"1","autoscaling.knative.dev/maxScale":"10"}},
			"spec":{"containers":[{"resources":{"limits":{"memory":"512Mi"}}}]}}}}`), &r))
	for _, k := range Kinds {
		assert.Len(t, k.cells(r), len(k.Columns), k.Name)
	}
	got := serviceCells(r)
	assert.Equal(t, "1/10", got[3])
	assert.Equal(t, "512MB", got[4])
}

func TestMem(t *testing.T) {
	for limit, want := range map[string]string{"1Gi": "1024MB", "512Mi": "512MB", "": "-", "2G": "2G"} {
		var r resource
		require.NoError(t, json.Unmarshal([]byte(`{"spec":{"template":{"spec":{"containers":[{"resources":{"limits":{"memory":"`+limit+`"}}}]}}}}`), &r))
		assert.Equal(t, want, mem(r), limit)
	}
}

func TestWriteYAML(t *testing.T) {
	var v any
	require.NoError(t, json.Unmarshal([]byte(`{"kind":"Service","status":"True","metadata":{"annotations":{"minScale":"1"}},"spec":{"containers":[{"image":"a:1","ports":[]}],"empty":{}},"n":2}`), &v))
	var b strings.Builder
	writeYAML(&b, v, "")
	want := `kind: Service
metadata:
  annotations:
    minScale: "1"
n: 2
spec:
  containers:
  - image: "a:1"
    ports: []
  empty: {}
status: "True"
`
	assert.Equal(t, want, b.String())
}

func TestDemo(t *testing.T) {
	ps, err := Demo.Projects()
	require.NoError(t, err)
	assert.NotEmpty(t, ps)
	for _, k := range Kinds {
		rs, err := Demo.List(k, "demo-prod")
		require.NoError(t, err)
		require.NotEmpty(t, rs, k.Name)
		for _, r := range rs {
			assert.Len(t, r.Cells, len(k.Columns))
			_, err := Demo.yaml(r)
			assert.NoError(t, err)
		}
	}
	rs, err := Demo.List(Jobs, "demo-staging")
	assert.NoError(t, err)
	assert.Empty(t, rs)
	assert.Error(t, Demo.Run(Console, Resource{Kind: Services}))
}
