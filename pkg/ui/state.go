package ui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/pavel-durov/r9s/pkg/cloudrun"
)

// state is what the UI shows: a project, a kind, a name filter, and the resources loaded per kind.
// It holds no widgets, so its rules are tested directly.
type state struct {
	project string
	kind    *cloudrun.Kind
	filter  string
	loaded  map[*cloudrun.Kind][]cloudrun.Resource
}

func newState(project string) *state {
	return &state{project: project, kind: cloudrun.Services, loaded: map[*cloudrun.Kind][]cloudrun.Resource{}}
}

// pick switches to project, starting from its services with no filter and nothing loaded.
func (s *state) pick(project string) { *s = *newState(project) }

// nextKind cycles to the next kind and clears the filter. It reports whether that kind still needs loading.
func (s *state) nextKind() bool {
	s.kind = cloudrun.Kinds[(slices.Index(cloudrun.Kinds, s.kind)+1)%len(cloudrun.Kinds)]
	s.filter = ""
	_, ok := s.loaded[s.kind]
	return !ok
}

// store records a finished load. It reports whether to redraw: false when the
// project changed while loading, or the load was for a kind not on screen.
func (s *state) store(project string, k *cloudrun.Kind, rs []cloudrun.Resource) bool {
	if project != s.project {
		return false
	}
	s.loaded[k] = rs
	return k == s.kind
}

// visible returns the current kind's resources whose name contains the filter, ignoring case.
func (s *state) visible() []cloudrun.Resource {
	var vs []cloudrun.Resource
	for _, r := range s.loaded[s.kind] {
		if strings.Contains(strings.ToLower(r.Name), strings.ToLower(s.filter)) {
			vs = append(vs, r)
		}
	}
	return vs
}

// status is the default footer: how many resources are shown, and the active filter.
func (s *state) status() string {
	msg := fmt.Sprintf("%d %s", len(s.visible()), s.kind.Name)
	if s.filter != "" {
		msg += fmt.Sprintf("  (filter: %s, <esc> clears)", s.filter)
	}
	return msg
}
