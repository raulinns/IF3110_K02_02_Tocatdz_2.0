package ui

import (
	"context"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/admin"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

// AdminAdapter menghubungkan admin.Service ke interface halaman admin, seperti ExplorerAdapter untuk explorer.
type AdminAdapter struct{ service *admin.Service }

var (
	_ UserAdmin       = (*AdminAdapter)(nil)
	_ GroupAdmin      = (*AdminAdapter)(nil)
	_ ConnectionAdmin = (*AdminAdapter)(nil)
	_ GrantAdmin      = (*AdminAdapter)(nil)
)

func NewAdminAdapter(service *admin.Service) (*AdminServices, error) {
	if service == nil {
		return nil, security.ErrInvalidInput
	}
	adapter := &AdminAdapter{service: service}
	return &AdminServices{Users: adapter, Groups: adapter, Connections: adapter, Grants: adapter}, nil
}

func adminUser(user admin.User) AdminUser {
	return AdminUser{ID: user.ID, Username: user.Username, Active: user.Active, PlatformAdmin: user.PlatformAdmin}
}

func (a *AdminAdapter) Users(ctx context.Context, principal security.Principal) ([]AdminUser, error) {
	users, err := a.service.Users(ctx, principal)
	if err != nil {
		return nil, err
	}
	result := make([]AdminUser, 0, len(users))
	for _, user := range users {
		result = append(result, adminUser(user))
	}
	return result, nil
}

func (a *AdminAdapter) CreateUser(ctx context.Context, principal security.Principal, user NewUser) error {
	return a.service.CreateUser(ctx, principal, admin.NewUser{Username: user.Username, Password: user.Password, PlatformAdmin: user.PlatformAdmin})
}

func (a *AdminAdapter) SetUserActive(ctx context.Context, principal security.Principal, userID security.ID, active bool) error {
	return a.service.SetUserActive(ctx, principal, userID, active)
}

func (a *AdminAdapter) Groups(ctx context.Context, principal security.Principal) ([]AdminGroup, error) {
	groups, err := a.service.Groups(ctx, principal)
	if err != nil {
		return nil, err
	}
	result := make([]AdminGroup, 0, len(groups))
	for _, group := range groups {
		item := AdminGroup{ID: group.ID, Name: group.Name, Members: make([]AdminUser, 0, len(group.Members))}
		for _, member := range group.Members {
			item.Members = append(item.Members, adminUser(member))
		}
		result = append(result, item)
	}
	return result, nil
}

func (a *AdminAdapter) CreateGroup(ctx context.Context, principal security.Principal, name string) error {
	return a.service.CreateGroup(ctx, principal, name)
}

func (a *AdminAdapter) AddMember(ctx context.Context, principal security.Principal, groupID security.ID, username string) error {
	return a.service.AddMember(ctx, principal, groupID, username)
}

func (a *AdminAdapter) RemoveMember(ctx context.Context, principal security.Principal, groupID, userID security.ID) error {
	return a.service.RemoveMember(ctx, principal, groupID, userID)
}

func (a *AdminAdapter) Connections(ctx context.Context, principal security.Principal) ([]AdminConnection, error) {
	connections, err := a.service.Connections(ctx, principal)
	if err != nil {
		return nil, err
	}
	result := make([]AdminConnection, 0, len(connections))
	for _, connection := range connections {
		result = append(result, AdminConnection{ID: connection.ID, Name: connection.Name, Engine: connection.Engine})
	}
	return result, nil
}

func (a *AdminAdapter) CreateConnection(ctx context.Context, principal security.Principal, connection NewConnection) error {
	return a.service.CreateConnection(ctx, principal, connection.Engine, connection.Name, connection.DSN)
}

func (a *AdminAdapter) Grants(ctx context.Context, principal security.Principal) ([]AdminGrant, error) {
	grants, err := a.service.Grants(ctx, principal)
	if err != nil {
		return nil, err
	}
	result := make([]AdminGrant, 0, len(grants))
	for _, grant := range grants {
		result = append(result, AdminGrant{GroupID: grant.GroupID, GroupName: grant.GroupName, DatabaseID: grant.DatabaseID, DatabaseName: grant.DatabaseName, Access: grant.Access})
	}
	return result, nil
}

func (a *AdminAdapter) SetGrant(ctx context.Context, principal security.Principal, groupID, databaseID security.ID, access security.DatabaseAccess) error {
	return a.service.SetGrant(ctx, principal, groupID, databaseID, access)
}

func (a *AdminAdapter) RevokeGrant(ctx context.Context, principal security.Principal, groupID, databaseID security.ID) error {
	return a.service.RevokeGrant(ctx, principal, groupID, databaseID)
}
