package stratum

import (
	"github.com/block/proto-fleet/server/internal/infrastructure/secrets"
)

const (
	authMethod = "mining.authorize"
)

type AuthRequest struct {
	Username string        `json:"username"`
	Password *secrets.Text `json:"password,omitempty"`
}

func (r *AuthRequest) MarshalParams() []any {
	ret := []any{r.Username}
	if r.Password != nil {
		ret = append(ret, r.Password.Value())
	}
	return ret
}

func (r *AuthRequest) Method() string {
	return authMethod
}

type AuthResponse bool
