package tenant

import "time"

const (
	PlanFree = "free"
	PlanPro  = "pro"
)

type Tenant struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Plan      string    `json:"plan"`
	CreatedAt time.Time `json:"createdAt"`
}
