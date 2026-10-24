package ui

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

const (
	maxGroupNameRunes      = 64
	maxConnectionNameRunes = 64
	maxConnectionDSNBytes  = 4 << 10
)

type AdminUser struct {
	ID            security.ID
	Username      string
	Active        bool
	PlatformAdmin bool
}

type AdminGroup struct {
	ID      security.ID
	Name    string
	Members []AdminUser
}

// DSN tidak pernah dikirim balik ke UI setelah disimpan
type AdminConnection struct {
	ID   security.ID
	Name string
}

type AdminGrant struct {
	GroupID      security.ID
	GroupName    string
	DatabaseID   security.ID
	DatabaseName string
	Access       security.DatabaseAccess
}

func (g AdminGrant) AccessLabel() string {
	return DatabaseSummary{Access: g.Access}.AccessLabel()
}

type NewUser struct {
	Username      string
	Password      security.Secret
	PlatformAdmin bool
}

type NewConnection struct {
	Name string
	DSN  security.Secret
}

// Setiap implementasi wajib memanggil RequirePlatformAdmin sendiri dan menjalankan efek samping keamanan secara atomik
type UserAdmin interface {
	Users(ctx context.Context, principal security.Principal) ([]AdminUser, error)
	CreateUser(ctx context.Context, principal security.Principal, user NewUser) error
	SetUserActive(ctx context.Context, principal security.Principal, userID security.ID, active bool) error
}

type GroupAdmin interface {
	Groups(ctx context.Context, principal security.Principal) ([]AdminGroup, error)
	CreateGroup(ctx context.Context, principal security.Principal, name string) error
	AddMember(ctx context.Context, principal security.Principal, groupID security.ID, username string) error
	RemoveMember(ctx context.Context, principal security.Principal, groupID, userID security.ID) error
}

type ConnectionAdmin interface {
	Connections(ctx context.Context, principal security.Principal) ([]AdminConnection, error)
	CreateConnection(ctx context.Context, principal security.Principal, connection NewConnection) error
}

type GrantAdmin interface {
	Grants(ctx context.Context, principal security.Principal) ([]AdminGrant, error)
	SetGrant(ctx context.Context, principal security.Principal, groupID, databaseID security.ID, access security.DatabaseAccess) error
	RevokeGrant(ctx context.Context, principal security.Principal, groupID, databaseID security.ID) error
}

type AdminServices struct {
	Users       UserAdmin
	Groups      GroupAdmin
	Connections ConnectionAdmin
	Grants      GrantAdmin
}

type AdminView struct {
	Section string
	Data    any
	Error   string
}

type GrantsData struct {
	Grants      []AdminGrant
	Groups      []AdminGroup
	Connections []AdminConnection
}

type adminPage struct {
	section string
	title   string
	path    string
	load    func(context.Context, security.Principal) (any, error)
}

type AdminUI struct {
	services AdminServices
	guard    security.Middleware
	access   *DatabaseGuard
	renderer *Renderer
}

var _ security.RouteRegistrar = (*AdminUI)(nil)

func NewAdminUI(services AdminServices, guard security.Middleware, access *DatabaseGuard, renderer *Renderer) (*AdminUI, error) {
	for _, dependency := range []any{services.Users, services.Groups, services.Connections, services.Grants, guard, access, renderer} {
		if missingDependency(dependency) {
			return nil, security.ErrInvalidInput
		}
	}
	return &AdminUI{services: services, guard: guard, access: access, renderer: renderer}, nil
}

func (ui *AdminUI) pages() (users, groups, connections, grants adminPage) {
	users = adminPage{section: "users", title: "User", path: "/admin/users", load: func(ctx context.Context, p security.Principal) (any, error) {
		return ui.services.Users.Users(ctx, p)
	}}
	groups = adminPage{section: "groups", title: "Group", path: "/admin/groups", load: func(ctx context.Context, p security.Principal) (any, error) {
		return ui.services.Groups.Groups(ctx, p)
	}}
	connections = adminPage{section: "connections", title: "Koneksi database", path: "/admin/connections", load: func(ctx context.Context, p security.Principal) (any, error) {
		return ui.services.Connections.Connections(ctx, p)
	}}
	grants = adminPage{section: "grants", title: "Akses database", path: "/admin/grants", load: ui.loadGrants}
	return users, groups, connections, grants
}

func (ui *AdminUI) loadGrants(ctx context.Context, p security.Principal) (any, error) {
	grants, err := ui.services.Grants.Grants(ctx, p)
	if err != nil {
		return nil, err
	}
	groups, err := ui.services.Groups.Groups(ctx, p)
	if err != nil {
		return nil, err
	}
	connections, err := ui.services.Connections.Connections(ctx, p)
	if err != nil {
		return nil, err
	}
	return GrantsData{Grants: grants, Groups: groups, Connections: connections}, nil
}

func (ui *AdminUI) RegisterRoutes(mux *http.ServeMux) {
	users, groups, connections, grants := ui.pages()
	handle := func(pattern string, handler http.HandlerFunc) { mux.Handle(pattern, ui.guard.Protected(handler)) }
	handle("GET /admin", func(w http.ResponseWriter, r *http.Request) {
		if _, err := ui.access.RequireAdmin(r); err != nil {
			_ = ui.renderer.RenderError(w, r, err)
			return
		}
		http.Redirect(w, r, users.path, http.StatusSeeOther)
	})
	handle("GET /admin/users", ui.show(users))
	handle("POST /admin/users", ui.mutate(users, ui.createUser, "username", "password", "platform_admin"))
	handle("POST /admin/users/{userID}/active", ui.mutate(users, ui.setUserActive, "active"))
	handle("GET /admin/groups", ui.show(groups))
	handle("POST /admin/groups", ui.mutate(groups, ui.createGroup, "name"))
	handle("POST /admin/groups/{groupID}/members", ui.mutate(groups, ui.addMember, "username"))
	handle("POST /admin/groups/{groupID}/members/{userID}/remove", ui.mutate(groups, ui.removeMember))
	handle("GET /admin/connections", ui.show(connections))
	handle("POST /admin/connections", ui.mutate(connections, ui.createConnection, "name", "dsn"))
	handle("GET /admin/grants", ui.show(grants))
	handle("POST /admin/grants", ui.mutate(grants, ui.setGrant, "group_id", "database_id", "access"))
	handle("POST /admin/grants/revoke", ui.mutate(grants, ui.revokeGrant, "group_id", "database_id"))
}

func (ui *AdminUI) render(w http.ResponseWriter, r *http.Request, page adminPage, principal security.Principal, status int, message string) {
	data, err := page.load(r.Context(), principal)
	if err != nil {
		_ = ui.renderer.RenderError(w, r, err)
		return
	}
	_ = ui.renderer.Render(w, r, status, "admin/"+page.section, Page{Title: page.title, Content: AdminView{Section: page.section, Data: data, Error: message}})
}

func (ui *AdminUI) show(page adminPage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := ui.access.RequireAdmin(r)
		if err != nil {
			_ = ui.renderer.RenderError(w, r, err)
			return
		}
		ui.render(w, r, page, principal, http.StatusOK, "")
	}
}

type adminAction func(r *http.Request, principal security.Principal, values url.Values) error

// Kesalahan yang bisa diperbaiki admin ditampilkan di halaman yang sama; selain itu halaman error
func (ui *AdminUI) mutate(page adminPage, action adminAction, fields ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := ui.access.RequireAdmin(r)
		if err != nil {
			_ = ui.renderer.RenderError(w, r, err)
			return
		}
		values, err := readForm(r, fields...)
		if err == nil {
			err = action(r, principal, values)
		}
		if err == nil {
			http.Redirect(w, r, page.path, http.StatusSeeOther)
			return
		}
		for _, category := range []error{security.ErrInvalidInput, security.ErrConflict, security.ErrNotFound} {
			if errors.Is(err, category) {
				status, message := security.HTTPStatus(err)
				ui.render(w, r, page, principal, status, message)
				return
			}
		}
		_ = ui.renderer.RenderError(w, r, err)
	}
}

func formBool(values url.Values, field string) (bool, error) {
	switch values.Get(field) {
	case "true":
		return true, nil
	case "", "false":
		return false, nil
	}
	return false, security.ErrInvalidInput
}

func (ui *AdminUI) createUser(r *http.Request, principal security.Principal, values url.Values) error {
	username, err := security.NormalizeUsername(values.Get("username"))
	if err != nil {
		return err
	}
	password := security.NewSecret(values.Get("password"))
	if err := security.ValidateNewPassword(password); err != nil {
		return err
	}
	admin, err := formBool(values, "platform_admin")
	if err != nil {
		return err
	}
	return ui.services.Users.CreateUser(r.Context(), principal, NewUser{Username: username, Password: password, PlatformAdmin: admin})
}

func (ui *AdminUI) setUserActive(r *http.Request, principal security.Principal, values url.Values) error {
	userID, err := pathID(r, "userID")
	if err != nil {
		return err
	}
	if values.Get("active") == "" {
		return security.ErrInvalidInput
	}
	active, err := formBool(values, "active")
	if err != nil {
		return err
	}
	return ui.services.Users.SetUserActive(r.Context(), principal, userID, active)
}

func (ui *AdminUI) createGroup(r *http.Request, principal security.Principal, values url.Values) error {
	name, ok := formText(values, "name", maxGroupNameRunes)
	if !ok {
		return security.ErrInvalidInput
	}
	return ui.services.Groups.CreateGroup(r.Context(), principal, name)
}

func (ui *AdminUI) addMember(r *http.Request, principal security.Principal, values url.Values) error {
	groupID, err := pathID(r, "groupID")
	if err != nil {
		return err
	}
	username, err := security.NormalizeUsername(values.Get("username"))
	if err != nil {
		return err
	}
	return ui.services.Groups.AddMember(r.Context(), principal, groupID, username)
}

func (ui *AdminUI) removeMember(r *http.Request, principal security.Principal, _ url.Values) error {
	groupID, err := pathID(r, "groupID")
	if err != nil {
		return err
	}
	userID, err := pathID(r, "userID")
	if err != nil {
		return err
	}
	return ui.services.Groups.RemoveMember(r.Context(), principal, groupID, userID)
}

func (ui *AdminUI) createConnection(r *http.Request, principal security.Principal, values url.Values) error {
	name, ok := formText(values, "name", maxConnectionNameRunes)
	dsn := values.Get("dsn")
	if !ok || dsn == "" || len(dsn) > maxConnectionDSNBytes {
		return security.ErrInvalidInput
	}
	return ui.services.Connections.CreateConnection(r.Context(), principal, NewConnection{Name: name, DSN: security.NewSecret(dsn)})
}

func formAccess(values url.Values) (security.DatabaseAccess, error) {
	access := security.DatabaseAccess(values.Get("access"))
	if access != security.AccessViewOnly && access != security.AccessViewEdit {
		return "", security.ErrInvalidInput
	}
	return access, nil
}

func grantTarget(values url.Values) (security.ID, security.ID, error) {
	groupID, err := formID(values, "group_id")
	if err != nil {
		return "", "", err
	}
	databaseID, err := formID(values, "database_id")
	if err != nil {
		return "", "", err
	}
	return groupID, databaseID, nil
}

func (ui *AdminUI) setGrant(r *http.Request, principal security.Principal, values url.Values) error {
	groupID, databaseID, err := grantTarget(values)
	if err != nil {
		return err
	}
	access, err := formAccess(values)
	if err != nil {
		return err
	}
	return ui.services.Grants.SetGrant(r.Context(), principal, groupID, databaseID, access)
}

func (ui *AdminUI) revokeGrant(r *http.Request, principal security.Principal, values url.Values) error {
	groupID, databaseID, err := grantTarget(values)
	if err != nil {
		return err
	}
	return ui.services.Grants.RevokeGrant(r.Context(), principal, groupID, databaseID)
}
