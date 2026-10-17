package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pavel-durov/r9s/pkg/cloudrun"
)

func TestState(t *testing.T) {
	api := cloudrun.Resource{Name: "API"}
	web := cloudrun.Resource{Name: "web"}
	s := newState("a")
	assert.Equal(t, cloudrun.Services, s.kind)

	assert.False(t, s.store("b", cloudrun.Services, []cloudrun.Resource{api}), "stale project")
	assert.Empty(t, s.visible())

	assert.True(t, s.store("a", cloudrun.Services, []cloudrun.Resource{api, web}))
	s.filter = "ap"
	assert.Equal(t, []cloudrun.Resource{api}, s.visible(), "filter ignores case")
	assert.Equal(t, "1 services  (filter: ap, <esc> clears)", s.status())

	assert.False(t, s.store("a", cloudrun.Jobs, nil), "kind not on screen")
	assert.False(t, s.nextKind(), "jobs already loaded")
	assert.Equal(t, cloudrun.Jobs, s.kind)
	assert.Empty(t, s.filter)
	assert.False(t, s.nextKind())
	assert.Equal(t, cloudrun.Services, s.kind)

	s.pick("c")
	assert.Equal(t, "c", s.project)
	assert.Empty(t, s.visible())
	assert.True(t, s.nextKind(), "nothing loaded after pick")
}
