package tenant

import "github.com/skaphos/berth/internal/auth"

type Resolver interface {
	ResolveTenant(identity *auth.Identity) (string, error)
}
