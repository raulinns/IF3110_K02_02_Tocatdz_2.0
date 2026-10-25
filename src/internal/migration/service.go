package migration

import (
	"context"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/dbms"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type Config struct {
	Repository  Repository
	Coordinator Coordinator
	Validator   Validator
	Authorizer  security.Authorizer
	Accounts    Accounts
	IDs         IDs
	Clock       security.Clock
}
type Service struct{ deps Config }

var _ Application = (*Service)(nil)

func (s *Service) Capabilities(ctx context.Context, id security.ID) (dbms.Capabilities, error) {
	resolver, ok := s.deps.Coordinator.(dbms.CapabilityResolver)
	if !ok {
		return dbms.Features(dbms.PostgreSQL), nil
	}
	capabilities, err := resolver.Capabilities(ctx, id)
	if err != nil {
		return dbms.Capabilities{}, safeError(err)
	}
	return capabilities, nil
}

func NewService(cfg Config) (*Service, error) {
	for _, dep := range []any{cfg.Repository, cfg.Coordinator, cfg.Validator, cfg.Authorizer, cfg.Accounts, cfg.IDs, cfg.Clock} {
		if missing(dep) {
			return nil, security.ErrInvalidInput
		}
	}
	return &Service{deps: cfg}, nil
}
func (s *Service) authorize(ctx context.Context, req Request, action security.DatabaseAction) error {
	if ctx.Err() != nil {
		return security.ErrUnavailable
	}
	p := req.Principal
	if !security.ValidID(p.UserID) || !security.ValidID(p.SessionID) || p.AuthVersion == 0 || p.CSRFEpoch == 0 {
		return security.ErrUnauthenticated
	}
	if !security.ValidID(req.DatabaseID) {
		return security.ErrInvalidInput
	}
	return safeError(s.deps.Authorizer.RequireDatabase(ctx, p, req.DatabaseID, action))
}
func validSummary(record Summary, database security.ID) bool {
	if record.DatabaseID != database || !security.ValidID(record.ID) || strings.TrimSpace(record.Title) == "" || !utf8.ValidString(record.Title) || utf8.RuneCountInString(record.Title) > MaxTitleRunes || record.UpdatedAt.IsZero() {
		return false
	}
	if record.Status == DraftStatus {
		return record.Version == 0 && record.ExecutedAt.IsZero()
	}
	return (record.Status == Pending || record.Status == Applied || record.Status == Failed) && record.Version > 0 && security.ValidID(record.ActorID) && record.Actor != "" && len(record.Actor) <= 256 && utf8.ValidString(record.Actor) && !record.ExecutedAt.IsZero()
}
func validRecord(record Migration, database security.ID) bool {
	if !validSummary(record.Summary, database) || record.Revision == 0 || ValidateDraft(Draft{Title: record.Title, SQL: record.SQL}) != nil {
		return false
	}
	if record.Status == DraftStatus {
		return record.ExecutionID == "" && record.Failure == FailureNone
	}
	if !security.ValidID(record.ExecutionID) || record.Checksum != Checksum(record.SQL) {
		return false
	}
	if record.Status == Failed {
		return record.Failure == FailureSQL || record.Failure == FailureConnection || record.Failure == FailureInterrupted
	}
	return record.Failure == FailureNone
}
func (s *Service) find(ctx context.Context, req Request, id security.ID) (Migration, error) {
	if !security.ValidID(id) {
		return Migration{}, security.ErrInvalidInput
	}
	record, err := s.deps.Repository.Find(ctx, req.DatabaseID, id)
	if err != nil {
		return Migration{}, safeError(err)
	}
	if record.ID != id || !validRecord(record, req.DatabaseID) {
		return Migration{}, security.ErrUnavailable
	}
	return record, nil
}
func (s *Service) History(ctx context.Context, req Request, query HistoryQuery) (HistoryPage, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.authorize(ctx, req, security.ActionViewHistory); err != nil {
		return HistoryPage{}, err
	}
	query, offset, err := pagination(query)
	if err != nil {
		return HistoryPage{}, err
	}
	items, err := s.deps.Repository.History(ctx, req.DatabaseID, HistoryOptions{Offset: offset, FetchLimit: query.Limit + 1})
	if err != nil {
		return HistoryPage{}, safeError(err)
	}
	if len(items) > query.Limit+1 {
		return HistoryPage{}, security.ErrUnavailable
	}
	seen := map[security.ID]bool{}
	versions := map[int64]bool{}
	for i, item := range items {
		if !validSummary(item, req.DatabaseID) || seen[item.ID] {
			return HistoryPage{}, security.ErrUnavailable
		}
		seen[item.ID] = true
		if item.Version > 0 {
			if versions[item.Version] {
				return HistoryPage{}, security.ErrUnavailable
			}
			versions[item.Version] = true
		}
		if i > 0 && !historyBefore(items[i-1], item) {
			return HistoryPage{}, security.ErrUnavailable
		}
	}
	if err := s.authorize(ctx, req, security.ActionViewHistory); err != nil {
		return HistoryPage{}, err
	}
	return HistoryPage{Items: append([]Summary{}, items[:min(len(items), query.Limit)]...), Page: query.Page, Limit: query.Limit, HasNext: len(items) > query.Limit}, nil
}
func (s *Service) Migration(ctx context.Context, req Request, id security.ID) (Migration, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.authorize(ctx, req, security.ActionViewMigration); err != nil {
		return Migration{}, err
	}
	record, err := s.find(ctx, req, id)
	if err != nil {
		return Migration{}, err
	}
	if err := s.authorize(ctx, req, security.ActionViewMigration); err != nil {
		return Migration{}, err
	}
	return record, nil
}
func (s *Service) CreateDraft(ctx context.Context, req Request, draft Draft) (security.ID, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.authorize(ctx, req, security.ActionCreateMigration); err != nil {
		return "", err
	}
	if err := ValidateDraft(draft); err != nil {
		return "", err
	}
	id, err := s.deps.IDs.GenerateID()
	if err != nil || !security.ValidID(id) {
		return "", security.ErrUnavailable
	}
	record := Migration{Summary: Summary{ID: id, DatabaseID: req.DatabaseID, Title: draft.Title, Status: DraftStatus, UpdatedAt: s.deps.Clock.Now().UTC()}, SQL: draft.SQL, Revision: 1}
	if err := s.authorize(ctx, req, security.ActionCreateMigration); err != nil {
		return "", err
	}
	if err := s.deps.Repository.Create(ctx, req, record); err != nil {
		return "", safeError(err)
	}
	return id, nil
}
func (s *Service) UpdateDraft(ctx context.Context, req Request, id security.ID, draft Draft, revision uint64) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.authorize(ctx, req, security.ActionUpdateMigration); err != nil {
		return err
	}
	if revision == 0 || revision == math.MaxUint64 || ValidateDraft(draft) != nil {
		return security.ErrInvalidInput
	}
	record, err := s.find(ctx, req, id)
	if err != nil {
		return err
	}
	if record.Status != DraftStatus || record.Revision != revision {
		return security.ErrConflict
	}
	if err := s.authorize(ctx, req, security.ActionUpdateMigration); err != nil {
		return err
	}
	return safeError(s.deps.Repository.Update(ctx, req, id, revision, draft, s.deps.Clock.Now().UTC()))
}

func historyBefore(a, b Summary) bool {
	if (a.Status == DraftStatus) != (b.Status == DraftStatus) {
		return a.Status == DraftStatus
	}
	if a.Version != b.Version {
		return a.Version > b.Version
	}
	if !a.UpdatedAt.Equal(b.UpdatedAt) {
		return a.UpdatedAt.After(b.UpdatedAt)
	}
	return a.ID < b.ID
}
