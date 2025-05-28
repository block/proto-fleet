package stratum

import (
	secret "github.com/rsjethani/secret/v3"
)

const (
	authMethod = "mining.authorize"
)

type AuthRequest struct {
	Username string       `json:"username"`
	Password *secret.Text `json:"password,omitempty"`
}

func (r *AuthRequest) MarshalParams() []any {
	ret := []any{r.Username}
	if r.Password != nil {
		ret = append(ret, r.Password.Secret())
	}
	return ret
}

func (r *AuthRequest) Method() string {
	return authMethod
}

type AuthResponse bool
