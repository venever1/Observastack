package tenant

import "time"

const (
	RoleAdmin  = "admin"
	RoleMember = "member"
)

type Member struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenantId"`
	UserID    string    `json:"userId"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"createdAt"`
}
