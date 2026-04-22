package command

import (
	"testing"

	"github.com/stretchr/testify/assert"

	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

func TestActorTypeFromSession(t *testing.T) {
	cases := []struct {
		name string
		info *session.Info
		want activitymodels.ActorType
	}{
		{"nil info", nil, ""},
		{"empty actor defaults to user via activity service defaulting", &session.Info{}, ""},
		{"scheduler actor", &session.Info{Actor: session.ActorScheduler}, activitymodels.ActorScheduler},
		{"system actor", &session.Info{Actor: session.ActorSystem}, activitymodels.ActorSystem},
		{"unknown actor falls back to empty", &session.Info{Actor: session.Actor("mystery")}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, actorTypeFromSession(tc.info))
		})
	}
}
