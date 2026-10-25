package migration

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

const (
	PlanTimeout  = 10 * time.Second
	MaxPlanBytes = 256 << 10
	MaxPlanNodes = 1024
)

type PlanRejection string

func (PlanRejection) Error() string { return "Analisis query ditolak" }
func (PlanRejection) Unwrap() error { return security.ErrInvalidInput }
func (r PlanRejection) Message() string {
	switch r {
	case "statement":
		return "Pilih nomor statement SELECT, INSERT, UPDATE, atau DELETE; perintah DDL tidak memiliki execution plan."
	case "analyze":
		return "EXPLAIN ANALYZE hanya tersedia untuk SELECT baca saja; gunakan EXPLAIN untuk query mutasi."
	case "query":
		return "Query tidak dapat dianalisis. Pastikan objek sudah ada dan SQL sesuai dengan kondisi database saat ini."
	case "timeout":
		return "Analisis melewati batas waktu atau sedang menunggu lock. Coba query yang lebih kecil."
	default:
		return "Hasil execution plan terlalu besar untuk ditampilkan."
	}
}

type PlanOptions struct {
	Revision  uint64
	Statement int
	Analyze   bool
}
type PlanQuery struct {
	SQL     security.Secret
	Analyze bool
}
type PlanNode struct {
	NodeType      string     `json:"Node Type"`
	Relation      string     `json:"Relation Name,omitempty"`
	Schema        string     `json:"Schema,omitempty"`
	Index         string     `json:"Index Name,omitempty"`
	Join          string     `json:"Join Type,omitempty"`
	Strategy      string     `json:"Strategy,omitempty"`
	StartupCost   float64    `json:"Startup Cost"`
	TotalCost     float64    `json:"Total Cost"`
	Rows          float64    `json:"Plan Rows"`
	Width         float64    `json:"Plan Width"`
	ActualStartup *float64   `json:"Actual Startup Time,omitempty"`
	ActualTotal   *float64   `json:"Actual Total Time,omitempty"`
	ActualRows    *float64   `json:"Actual Rows,omitempty"`
	ActualLoops   *float64   `json:"Actual Loops,omitempty"`
	Plans         []PlanNode `json:"Plans,omitempty"`
}
type PlanResult struct {
	Plan        PlanNode        `json:"Plan"`
	PlanningMS  *float64        `json:"Planning Time,omitempty"`
	ExecutionMS *float64        `json:"Execution Time,omitempty"`
	Statement   int             `json:"-"`
	Analyze     bool            `json:"-"`
	Revision    uint64          `json:"-"`
	Checksum    [32]byte        `json:"-"`
	SQL         security.Secret `json:"-"`
}
type PlanTarget interface {
	Explain(context.Context, PlanQuery) ([]byte, error)
}
type PlanCoordinator interface {
	WithPlanLock(context.Context, security.ID, func(context.Context, PlanTarget) error) error
}
type PlanApplication interface {
	SupportsPlans() bool
	Analyze(context.Context, Request, security.ID, PlanOptions) (PlanResult, error)
}

type PlanValidator interface {
	PlanStatement(context.Context, security.Secret, int, bool) (security.Secret, error)
}

func (s *Service) SupportsPlans() bool {
	_, ok := s.deps.Coordinator.(PlanCoordinator)
	_, parser := s.deps.Validator.(PlanValidator)
	return ok && parser
}

// Analisis memakai SQL draft tersimpan dan tidak mereservasi versi atau menulis receipt.
func (s *Service) Analyze(ctx context.Context, req Request, id security.ID, options PlanOptions) (PlanResult, error) {
	ctx, cancel := context.WithTimeout(ctx, PlanTimeout)
	defer cancel()
	if err := s.authorize(ctx, req, security.ActionAnalyzeMigration); err != nil {
		return PlanResult{}, err
	}
	if !security.ValidID(id) || options.Revision == 0 || options.Statement < 1 || options.Statement > 1000 {
		return PlanResult{}, security.ErrInvalidInput
	}
	if !s.SupportsPlans() {
		return PlanResult{}, security.ErrUnavailable
	}
	var result PlanResult
	err := s.deps.Coordinator.(PlanCoordinator).WithPlanLock(ctx, req.DatabaseID, func(ctx context.Context, target PlanTarget) error {
		if err := s.authorize(ctx, req, security.ActionAnalyzeMigration); err != nil {
			return err
		}
		record, err := s.find(ctx, req, id)
		if err != nil {
			return err
		}
		if record.Status != DraftStatus || record.Revision != options.Revision {
			return security.ErrConflict
		}
		sql, err := s.deps.Validator.(PlanValidator).PlanStatement(ctx, record.SQL, options.Statement, options.Analyze)
		if err != nil {
			return err
		}
		if missing(target) {
			return security.ErrUnavailable
		}
		if err := s.authorize(ctx, req, security.ActionAnalyzeMigration); err != nil {
			return err
		}
		raw, err := target.Explain(ctx, PlanQuery{SQL: sql, Analyze: options.Analyze})
		if err != nil {
			return err
		}
		parsed, err := DecodePlan(raw, options.Analyze)
		if err != nil {
			return err
		}
		if err := s.authorize(ctx, req, security.ActionAnalyzeMigration); err != nil {
			return err
		}
		latest, err := s.find(ctx, req, id)
		if err != nil {
			return err
		}
		if latest.Status != DraftStatus || latest.Revision != record.Revision || Checksum(latest.SQL) != Checksum(record.SQL) {
			return security.ErrConflict
		}
		parsed.Statement = options.Statement
		parsed.Revision = record.Revision
		parsed.Checksum = Checksum(record.SQL)
		parsed.SQL = sql
		result = parsed
		return nil
	})
	if err != nil {
		var reason PlanRejection
		if errors.As(err, &reason) {
			switch reason {
			case "statement", "analyze", "query", "timeout", "large":
				return PlanResult{}, reason
			}
		}
		if ctx.Err() != nil {
			return PlanResult{}, PlanRejection("timeout")
		}
		return PlanResult{}, safeValidationError(err)
	}
	if result.Revision == 0 {
		return PlanResult{}, security.ErrUnavailable
	}
	return result, nil
}

func DecodePlan(raw []byte, analyze bool) (PlanResult, error) {
	if len(raw) > MaxPlanBytes {
		return PlanResult{}, PlanRejection("large")
	}
	var records []PlanResult
	if !utf8.Valid(raw) || json.Unmarshal(raw, &records) != nil || len(records) != 1 {
		return PlanResult{}, security.ErrUnavailable
	}
	count := 0
	var check func(PlanNode, int) bool
	number := func(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 }
	optional := func(v *float64) bool { return v == nil || number(*v) }
	check = func(n PlanNode, depth int) bool {
		count++
		if count > MaxPlanNodes || depth > 64 || strings.TrimSpace(n.NodeType) == "" {
			return false
		}
		for _, value := range []string{n.NodeType, n.Relation, n.Schema, n.Index, n.Join, n.Strategy} {
			if !utf8.ValidString(value) || len(value) > 256 || strings.ContainsRune(value, 0) {
				return false
			}
		}
		for _, v := range []float64{n.StartupCost, n.TotalCost, n.Rows, n.Width} {
			if !number(v) {
				return false
			}
		}
		if n.TotalCost < n.StartupCost || !optional(n.ActualStartup) || !optional(n.ActualTotal) || !optional(n.ActualRows) || !optional(n.ActualLoops) {
			return false
		}
		if analyze && (n.ActualStartup == nil || n.ActualTotal == nil || n.ActualRows == nil || n.ActualLoops == nil) {
			return false
		}
		for _, child := range n.Plans {
			if !check(child, depth+1) {
				return false
			}
		}
		return true
	}
	result := records[0]
	if !check(result.Plan, 0) || !optional(result.PlanningMS) || !optional(result.ExecutionMS) || analyze && result.ExecutionMS == nil {
		return PlanResult{}, security.ErrUnavailable
	}
	result.Analyze = analyze
	return result, nil
}
