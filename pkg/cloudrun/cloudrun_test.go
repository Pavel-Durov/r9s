package cloudrun

import (
	"encoding/json"
	"slices"
	"testing"
	"time"
)

func TestRowAndAction(t *testing.T) {
	var rs []resource
	_ = json.Unmarshal([]byte(`[{"metadata":{"name":"api","labels":{"cloud.googleapis.com/location":"europe-west1"}},
		"spec":{"template":{"spec":{"containers":[{"resources":{"limits":{"memory":"1Gi"}}}]}}},
		"status":{"url":"https://api.run.app","latestReadyRevisionName":"api-001","conditions":[{"type":"Ready","status":"True"}]}},
		{"metadata":{"name":"etl"}}]`), &rs)
	if got := row("services", rs[0]); !slices.Equal(got, []string{"api", "europe-west1", "True", "-/-", "1024MB", "api-001", "https://api.run.app", ""}) {
		t.Fatal(got)
	}
	if got := row("jobs", rs[1]); !slices.Equal(got, []string{"etl", "", "?", "", ""}) {
		t.Fatal(got)
	}
	if cmd, paged := Command('s', "jobs", "etl", "us-central1", "p"); paged || !slices.Equal(cmd[:4], []string{"gcloud", "alpha", "run", "jobs"}) || cmd[len(cmd)-1] != "p" {
		t.Fatal(cmd)
	}
	if cmd, paged := Command('l', "services", "api", "eu", ""); !paged || !slices.Equal(cmd, []string{"gcloud", "run", "services", "logs", "read", "api", "--region", "eu", "--limit", "1000"}) {
		t.Fatal(cmd)
	}
}

func TestAge(t *testing.T) {
	if got := age(time.Now().Add(-50 * time.Hour).Format(time.RFC3339)); got != "2d2h" {
		t.Fatal(got)
	}
}

func TestGcloudError(t *testing.T) {
	stderr := "API not enabled. Enable (y/N)?\nERROR: (gcloud.run.services.list) PERMISSION_DENIED: denied on 'namespaces/p/services'. This command is authenticated as a@b.c\nreason: SERVICE_DISABLED\n"
	if got := gcloudError(stderr); got != "PERMISSION_DENIED: denied on 'namespaces/p/services'." {
		t.Fatal(got)
	}
}

func TestRowMatchesColumns(t *testing.T) {
	var r resource
	_ = json.Unmarshal([]byte(`{"metadata":{"name":"api"},
		"spec":{"template":{"metadata":{"annotations":{"autoscaling.knative.dev/minScale":"1","autoscaling.knative.dev/maxScale":"10"}},
			"spec":{"containers":[{"resources":{"limits":{"memory":"512Mi"}}}]}}}}`), &r)
	for _, kind := range Kinds {
		if got := row(kind, r); len(got) != len(Columns[kind]) {
			t.Fatalf("%s: %d cells, %d columns", kind, len(got), len(Columns[kind]))
		}
	}
	if got := row("services", r); got[3] != "1/10" || got[4] != "512MB" {
		t.Fatal(got)
	}
}

func TestMem(t *testing.T) {
	for limit, want := range map[string]string{"1Gi": "1024MB", "512Mi": "512MB", "": "-", "2G": "2G"} {
		var r resource
		_ = json.Unmarshal([]byte(`{"spec":{"template":{"spec":{"containers":[{"resources":{"limits":{"memory":"`+limit+`"}}}]}}}}`), &r)
		if got := mem(r); got != want {
			t.Errorf("%q: got %q, want %q", limit, got, want)
		}
	}
}
