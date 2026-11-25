package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/admin"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/dbms"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

func (cfg AdminConfig) createUser(w http.ResponseWriter, r *http.Request, actor security.Principal) error {
	var input struct {
		Username      string `json:"username"`
		Password      string `json:"password"`
		PlatformAdmin bool   `json:"platform_admin"`
	}
	if err := DecodeJSON(w, r, &input); err != nil {
		return err
	}
	user, err := cfg.Service.CreateUserRecord(r.Context(), actor, admin.NewUser{
		Username: input.Username, Password: security.NewSecret(input.Password), PlatformAdmin: input.PlatformAdmin,
	})
	if err != nil {
		return err
	}
	w.Header().Set("Location", Prefix+adminUsers+"/"+string(user.ID))
	return WriteJSON(w, http.StatusCreated, userResponse(user))
}

func (cfg AdminConfig) updateUser(w http.ResponseWriter, r *http.Request, actor security.Principal) error {
	id, err := PathID(r, "user_id")
	if err != nil {
		return err
	}
	var input struct {
		Active   json.RawMessage `json:"active"`
		Password json.RawMessage `json:"password"`
	}
	if err := DecodeJSON(w, r, &input); err != nil {
		return err
	}
	active, err := adminPatchValue[bool](input.Active)
	if err != nil {
		return err
	}
	rawPassword, err := adminPatchValue[string](input.Password)
	if err != nil {
		return err
	}
	var password *security.Secret
	if rawPassword != nil {
		secret := security.NewSecret(*rawPassword)
		password = &secret
	}
	user, err := cfg.M2.UpdateUser(r.Context(), actor, id, active, password)
	if err != nil {
		return err
	}
	return WriteJSON(w, http.StatusOK, userResponse(user))
}

// Distinguish an omitted patch field from explicit null and the scalar zero value.
func adminPatchValue[T bool | string](raw json.RawMessage) (*T, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var value *T
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return nil, security.ErrMalformedRequest
	}
	return value, nil
}

type adminGroupInput struct {
	Name string `json:"name"`
}

func (cfg AdminConfig) createGroup(w http.ResponseWriter, r *http.Request, actor security.Principal) error {
	var input adminGroupInput
	if err := DecodeJSON(w, r, &input); err != nil {
		return err
	}
	group, err := cfg.Service.CreateGroupRecord(r.Context(), actor, input.Name)
	if err != nil {
		return err
	}
	w.Header().Set("Location", Prefix+adminGroups+"/"+string(group.ID))
	return WriteJSON(w, http.StatusCreated, groupResponse(admin.GroupSummary{ID: group.ID, Name: group.Name}))
}

func (cfg AdminConfig) renameGroup(w http.ResponseWriter, r *http.Request, actor security.Principal) error {
	id, err := PathID(r, "group_id")
	if err != nil {
		return err
	}
	var input adminGroupInput
	if err := DecodeJSON(w, r, &input); err != nil {
		return err
	}
	group, err := cfg.M2.RenameGroup(r.Context(), actor, id, input.Name)
	if err != nil {
		return err
	}
	return WriteJSON(w, http.StatusOK, groupResponse(group))
}

func (cfg AdminConfig) createConnection(w http.ResponseWriter, r *http.Request, actor security.Principal) error {
	var input struct {
		Name   string      `json:"name"`
		Engine dbms.Engine `json:"engine"`
		DSN    string      `json:"dsn"`
	}
	if err := DecodeJSON(w, r, &input); err != nil {
		return err
	}
	if input.Engine != dbms.PostgreSQL && input.Engine != dbms.MySQL {
		return security.ErrInvalidInput
	}
	connection, err := cfg.Service.CreateConnectionRecord(r.Context(), actor, input.Engine, input.Name, security.NewSecret(input.DSN))
	if err != nil {
		return err
	}
	w.Header().Set("Location", Prefix+adminConnections+"/"+string(connection.ID))
	return WriteJSON(w, http.StatusCreated, connectionResponse(connection))
}

func adminDelete(parameter string, remove func(context.Context, security.Principal, security.ID) error) adminEndpoint {
	return func(w http.ResponseWriter, r *http.Request, actor security.Principal) error {
		id, err := PathID(r, parameter)
		if err != nil {
			return err
		}
		if err := remove(r.Context(), actor, id); err != nil {
			return err
		}
		return adminNoContent(w)
	}
}

func (cfg AdminConfig) changeMember(change func(context.Context, security.Principal, security.ID, security.ID) (bool, error)) adminEndpoint {
	return func(w http.ResponseWriter, r *http.Request, actor security.Principal) error {
		groupID, userID, err := adminGroupIDs(r, "user_id")
		if err != nil {
			return err
		}
		start := time.Now()
		changed, err := change(r.Context(), actor, groupID, userID)
		if err != nil {
			return err
		}
		if changed {
			cfg.logAdminChange(r, actor, "admin.membership_changed", adminMember, http.StatusNoContent, start, groupID, "", userID)
		}
		return adminNoContent(w)
	}
}

func (cfg AdminConfig) setLeader(w http.ResponseWriter, r *http.Request, actor security.Principal) error {
	groupID, err := PathID(r, "group_id")
	if err != nil {
		return err
	}
	var input struct {
		UserID security.ID `json:"user_id"`
	}
	if err := DecodeJSON(w, r, &input); err != nil {
		return err
	}
	start := time.Now()
	group, changed, err := cfg.M2.SetLeader(r.Context(), actor, groupID, input.UserID)
	if err != nil {
		return err
	}
	if changed {
		cfg.logAdminChange(r, actor, "admin.leader_changed", adminGroup+"/leader", http.StatusOK, start, groupID, "", input.UserID)
	}
	return WriteJSON(w, http.StatusOK, groupResponse(group))
}

func (cfg AdminConfig) putGrant(w http.ResponseWriter, r *http.Request, actor security.Principal) error {
	groupID, databaseID, err := adminGroupIDs(r, "database_id")
	if err != nil {
		return err
	}
	var input struct {
		Access security.DatabaseAccess `json:"access"`
	}
	if err := DecodeJSON(w, r, &input); err != nil {
		return err
	}
	start := time.Now()
	grant, changed, err := cfg.M2.PutGrant(r.Context(), actor, groupID, databaseID, input.Access)
	if err != nil {
		return err
	}
	if changed {
		cfg.logAdminChange(r, actor, "admin.grant_changed", adminGrant, http.StatusOK, start, groupID, databaseID, "")
	}
	return WriteJSON(w, http.StatusOK, grantResponse(grant))
}

func (cfg AdminConfig) revokeGrant(w http.ResponseWriter, r *http.Request, actor security.Principal) error {
	groupID, databaseID, err := adminGroupIDs(r, "database_id")
	if err != nil {
		return err
	}
	start := time.Now()
	if err := cfg.Service.RevokeGrant(r.Context(), actor, groupID, databaseID); err != nil {
		return err
	}
	cfg.logAdminChange(r, actor, "admin.grant_changed", adminGrant, http.StatusNoContent, start, groupID, databaseID, "")
	return adminNoContent(w)
}

func adminGroupIDs(r *http.Request, parameter string) (security.ID, security.ID, error) {
	groupID, err := PathID(r, "group_id")
	if err != nil {
		return "", "", err
	}
	second, err := PathID(r, parameter)
	return groupID, second, err
}

func adminNoContent(w http.ResponseWriter) error {
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (cfg AdminConfig) logAdminChange(r *http.Request, actor security.Principal, event, route string, status int, start time.Time, group, database, user security.ID) {
	var databaseID, userID any
	if database != "" {
		databaseID = database
	}
	if user != "" {
		userID = user
	}
	cfg.Logger.InfoContext(r.Context(), "Admin mutation committed",
		"event", event, "actor", actor.UserID, "database_id", databaseID, "group_id", group, "user_id", userID,
		"status", "succeeded", "request_id", security.RequestIDFromContext(r.Context()), "trace_id", nil, "span_id", nil,
		"http_method", r.Method, "http_route", Prefix+route, "http_status", status,
		"duration_ms", float64(time.Since(start))/float64(time.Millisecond))
}
