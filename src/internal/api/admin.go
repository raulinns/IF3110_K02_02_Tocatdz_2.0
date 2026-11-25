package api

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"slices"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/admin"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/dbms"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/explorer"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type AdminConfig struct {
	Service *admin.Service
	M2      *admin.M2Service
	Logger  *slog.Logger
}

const (
	adminUsers       = "/admin/users"
	adminGroups      = "/admin/groups"
	adminConnections = "/admin/connections"
	adminGrants      = "/admin/grants"
	adminGroup       = adminGroups + "/{group_id}"
	adminMembers     = adminGroup + "/members"
	adminMember      = adminMembers + "/{user_id}"
	adminGrant       = adminGrants + "/{group_id}/{database_id}"
)

// AdminRoutes mounts the M1-backed subset until the M2 repository is available.
// Supplying M2 enables the full K1 admin surface. Append to Config.Routes.
func AdminRoutes(cfg AdminConfig) ([]Route, error) {
	if cfg.Service == nil || cfg.Logger == nil {
		return nil, fmt.Errorf("admin service and logger are required: %w", security.ErrInvalidInput)
	}
	routes := []Route{
		adminListRoute(adminUsers, adminList(cfg.Service.Users, compareUsers, userResponse)),
		adminRoute(http.MethodPost, adminUsers, JSONBody, cfg.createUser),
		adminRoute(http.MethodPost, adminGroups, JSONBody, cfg.createGroup),
		adminListRoute(adminConnections, adminList(cfg.Service.Connections, compareConnections, connectionResponse)),
		adminRoute(http.MethodPost, adminConnections, JSONBody, cfg.createConnection),
		adminListRoute(adminGrants, adminList(cfg.Service.Grants, compareGrants, grantResponse)),
		adminRoute(http.MethodDelete, adminGrant, NoBody, cfg.revokeGrant),
	}
	if cfg.M2 != nil {
		routes = append(routes,
			adminRoute(http.MethodPatch, adminUsers+"/{user_id}", JSONBody, cfg.updateUser),
			adminListRoute(adminGroups, adminList(cfg.M2.GroupSummaries, compareGroups, groupResponse)),
			adminRoute(http.MethodPatch, adminGroup, JSONBody, cfg.renameGroup),
			adminRoute(http.MethodDelete, adminGroup, NoBody, adminDelete("group_id", cfg.M2.DeleteGroup)),
			adminRoute(http.MethodDelete, adminConnections+"/{connection_id}", NoBody, adminDelete("connection_id", cfg.M2.DeleteConnection)),
			adminListRoute(adminMembers, cfg.members),
			adminRoute(http.MethodPut, adminMember, NoBody, cfg.changeMember(cfg.M2.AddMemberByID)),
			adminRoute(http.MethodDelete, adminMember, NoBody, cfg.changeMember(cfg.M2.RemoveMember)),
			adminRoute(http.MethodPut, adminGroup+"/leader", JSONBody, cfg.setLeader),
			adminRoute(http.MethodPut, adminGrant, JSONBody, cfg.putGrant),
		)
	}
	return routes, nil
}

type adminEndpoint func(http.ResponseWriter, *http.Request, security.Principal) error

func adminRoute(method, path string, body BodyMode, handle adminEndpoint) Route {
	return Route{Method: method, Path: path, Body: body, Handle: func(w http.ResponseWriter, r *http.Request) error {
		actor, ok := security.PrincipalFromContext(r.Context())
		if !ok {
			return security.ErrUnauthenticated
		}
		return handle(w, r, actor)
	}}
}

func adminListRoute(path string, handle adminEndpoint) Route {
	route := adminRoute(http.MethodGet, path, NoBody, handle)
	route.QueryParameters = []string{"page", "limit"}
	return route
}

func adminList[T, D any](read func(context.Context, security.Principal) ([]T, error), compare func(T, T) int, dto func(T) D) adminEndpoint {
	return func(w http.ResponseWriter, r *http.Request, actor security.Principal) error {
		page, limit, offset, err := explorer.ParsePagination(r.URL.Query())
		if err != nil {
			return security.ErrMalformedRequest
		}
		records, err := read(r.Context(), actor)
		if err != nil {
			return err
		}
		records = slices.Clone(records)
		slices.SortFunc(records, compare)
		start := min(int(offset), len(records))
		end := min(start+limit, len(records))
		items := make([]D, 0, end-start)
		for _, record := range records[start:end] {
			items = append(items, dto(record))
		}
		return WriteJSON(w, http.StatusOK, struct {
			pageInfo
			Items []D `json:"items"`
		}{pageInfo: pageInfo{Page: page, Limit: limit, HasNext: end < len(records)}, Items: items})
	}
}

func (cfg AdminConfig) members(w http.ResponseWriter, r *http.Request, actor security.Principal) error {
	id, err := PathID(r, "group_id")
	if err != nil {
		return err
	}
	read := func(ctx context.Context, actor security.Principal) ([]admin.User, error) {
		return cfg.M2.GroupMembers(ctx, actor, id)
	}
	return adminList(read, compareUsers, userResponse)(w, r, actor)
}

func compareUsers(a, b admin.User) int {
	return cmp.Or(cmp.Compare(a.Username, b.Username), cmp.Compare(a.ID, b.ID))
}

func compareGroups(a, b admin.GroupSummary) int {
	return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.ID, b.ID))
}

func compareConnections(a, b admin.Connection) int {
	return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.ID, b.ID))
}

func compareGrants(a, b admin.Grant) int {
	return cmp.Or(cmp.Compare(a.GroupID, b.GroupID), cmp.Compare(a.DatabaseID, b.DatabaseID))
}

func userResponse(user admin.User) UserResponse {
	return UserResponse{ID: user.ID, Username: user.Username, Active: user.Active, PlatformAdmin: user.PlatformAdmin}
}

type adminGroupResponse struct {
	ID          security.ID  `json:"id"`
	Name        string       `json:"name"`
	LeaderID    *security.ID `json:"leader_user_id"`
	MemberCount int          `json:"member_count"`
}

func groupResponse(group admin.GroupSummary) adminGroupResponse {
	result := adminGroupResponse{ID: group.ID, Name: group.Name, MemberCount: group.MemberCount}
	if group.LeaderID != "" {
		result.LeaderID = &group.LeaderID
	}
	return result
}

type adminConnectionResponse struct {
	ID     security.ID `json:"id"`
	Name   string      `json:"name"`
	Engine dbms.Engine `json:"engine"`
}

func connectionResponse(connection admin.Connection) adminConnectionResponse {
	return adminConnectionResponse{ID: connection.ID, Name: connection.Name, Engine: connection.Engine}
}

type adminGrantResponse struct {
	GroupID      security.ID             `json:"group_id"`
	GroupName    string                  `json:"group_name"`
	DatabaseID   security.ID             `json:"database_id"`
	DatabaseName string                  `json:"database_name"`
	Access       security.DatabaseAccess `json:"access"`
}

func grantResponse(grant admin.Grant) adminGrantResponse {
	return adminGrantResponse{
		GroupID: grant.GroupID, GroupName: grant.GroupName, DatabaseID: grant.DatabaseID,
		DatabaseName: grant.DatabaseName, Access: grant.Access,
	}
}
