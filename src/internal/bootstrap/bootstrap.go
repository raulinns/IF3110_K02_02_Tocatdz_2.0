package bootstrap

import (
	"context"
	"io"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/argon2"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/admin"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/api"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/config"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/explorer"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	mysqltarget "github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/mysql"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/observability/metrics"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/postgres"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/schemadiff"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/spa"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/targetdb"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/web"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/worker"
)

// Platform berisi handler HTTP siap pakai beserta sumber daya yang harus ditutup saat aplikasi berhenti.
type Platform struct {
	Handler  http.Handler
	worker   *migration.ReconcileWorker
	m2Worker *worker.Worker
	targets  *targetdb.Router
	pool     *pgxpool.Pool
	metrics  *metrics.Registry
}

func NewPasswordHasher() (*security.Argon2idHasher, error) {
	return security.NewArgon2idHasher(argon2.IDKey)
}

// Build merakit seluruh komponen di atas PostgreSQL; logger dipakai middleware fondasi yang sudah ada.
func Build(ctx context.Context, logger *slog.Logger, cfg config.Config, sec config.Security) (*Platform, error) {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	pool, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	platform := &Platform{pool: pool}
	if err := platform.build(ctx, logger, sec); err != nil {
		platform.Close()
		return nil, err
	}
	return platform, nil
}

func (p *Platform) build(ctx context.Context, logger *slog.Logger, sec config.Security) error {
	if err := postgres.Migrate(ctx, p.pool); err != nil {
		return err
	}
	clock := security.SystemClock{}
	policy := security.DefaultSessionPolicy()
	tokens := security.NewTokenGenerator()
	store, err := postgres.NewStore(p.pool, clock)
	if err != nil {
		return err
	}
	hasher, err := NewPasswordHasher()
	if err != nil {
		return err
	}
	protector, err := security.NewAESGCMTokenProtector(sec.SessionKey)
	if err != nil {
		return err
	}
	csrf, err := security.NewHMACCSRFGuard(sec.PublicOrigin, sec.CSRFKey)
	if err != nil {
		return err
	}
	limiter, err := security.NewLoginLimiter(clock)
	if err != nil {
		return err
	}
	auth, err := security.NewAuthService(ctx, security.AuthServiceConfig{
		Credentials: store, Sessions: store, Passwords: hasher, Tokens: tokens, Protector: protector,
		CSRF: csrf, Throttle: limiter, Clock: clock, Policy: policy,
	})
	if err != nil {
		return err
	}
	cookies, err := security.NewSessionCookies(sec.PublicOrigin, sec.Production)
	if err != nil {
		return err
	}
	authorizer, err := security.NewBackendAuthorizer(store, store, clock, policy)
	if err != nil {
		return err
	}
	box, err := postgres.NewSecretBox(sec.ConnectionKey)
	if err != nil {
		return err
	}
	postgresTargets, err := postgres.NewTargets(p.pool, box)
	if err != nil {
		return err
	}
	mysqlTargets, err := mysqltarget.NewTargets(p.pool, box)
	if err != nil {
		postgresTargets.Close()
		return err
	}
	p.targets, err = targetdb.NewRouter(p.pool, postgresTargets, mysqlTargets)
	if err != nil {
		mysqlTargets.Close()
		postgresTargets.Close()
		return err
	}
	viewer, err := explorer.NewService(explorer.Config{Directory: store, Reader: p.targets, Authorizer: authorizer})
	if err != nil {
		return err
	}
	ledger, err := postgres.NewMigrationStore(p.pool)
	if err != nil {
		return err
	}
	reconciler, err := migration.NewReconciler(migration.ReconcilerConfig{Repository: ledger, Coordinator: p.targets, Clock: clock})
	if err != nil {
		return err
	}
	p.worker, err = migration.NewReconcileWorker(ledger, reconciler, migration.DefaultReconcileInterval)
	if err != nil {
		return err
	}
	postgresValidator := migration.NewPostgreSQLValidator()
	diff, err := schemadiff.NewService(schemadiff.Config{Reader: p.targets, Validator: postgresValidator, Authorizer: authorizer})
	if err != nil {
		return err
	}
	adminStore, err := postgres.NewAdminStore(p.pool, box, clock)
	if err != nil {
		return err
	}
	adminService, err := admin.NewService(admin.Config{Repository: adminStore, Authorizer: authorizer, Passwords: hasher, IDs: tokens, Checker: p.targets})
	if err != nil {
		return err
	}
	adminM2, err := admin.NewM2Service(adminService, adminStore)
	if err != nil {
		return err
	}
	workflow, err := postgres.NewM2Store(p.pool)
	if err != nil {
		return err
	}
	planner, err := migration.NewDefinitionPlanService(migration.DefinitionPlanConfig{
		Definitions: workflow, Authorizer: authorizer, Coordinator: p.targets, Validator: postgresValidator,
	})
	if err != nil {
		return err
	}
	workflowService, err := migration.NewWorkflowService(migration.WorkflowConfig{
		Store: workflow, Authorizer: authorizer, Engines: p.targets, IDs: tokens, Clock: clock,
	})
	if err != nil {
		return err
	}
	routes, err := apiRoutes(logger, adminService, adminM2, viewer, diff, p.targets, planner, workflowService)
	if err != nil {
		return err
	}
	v1, err := api.New(api.Config{
		Security: security.HTTPMiddlewareConfig{Auth: auth, Logout: auth, CSRF: csrf, Cookies: cookies, Throttle: limiter},
		Logger:   logger, Routes: routes, Auth: &api.AuthConfig{Leaders: workflow},
	})
	if err != nil {
		return err
	}
	frontend, err := spa.New()
	if err != nil {
		return err
	}
	p.metrics = newMetrics(p.pool, p.targets, workflow)
	if p.m2Worker, err = newM2Worker(workflow, p.targets, clock, logger, "server-embedded", 1, p.metrics); err != nil {
		return err
	}
	p.Handler = web.NewRouter(logger, v1, frontend)
	return nil
}

func apiRoutes(logger *slog.Logger, adminService *admin.Service, adminM2 *admin.M2Service, viewer explorer.Viewer,
	diff schemadiff.Application, engines api.EngineResolver, planner api.DefinitionPlanner, workflowService *migration.WorkflowService) ([]api.Route, error) {
	adminRoutes, err := api.AdminRoutes(api.AdminConfig{Service: adminService, M2: adminM2, Logger: logger})
	if err != nil {
		return nil, err
	}
	explorerRoutes, err := api.ExplorerRoutes(api.ExplorerConfig{Viewer: viewer, Diff: diff, Engines: engines})
	if err != nil {
		return nil, err
	}
	planRoutes, err := api.QueryPlanRoutes(planner)
	if err != nil {
		return nil, err
	}
	workflowRoutes, err := api.WorkflowRoutes(workflowService)
	if err != nil {
		return nil, err
	}
	routes := append(adminRoutes, explorerRoutes...)
	routes = append(routes, planRoutes...)
	return append(routes, workflowRoutes...), nil
}

// RunWorker menjalankan rekonsiliasi migration pending sampai ctx dibatalkan.
func (p *Platform) RunWorker(ctx context.Context) {
	if p.worker != nil {
		p.worker.Run(ctx)
	}
}

// MetricsHandler melayani /metrics format Prometheus; alamatnya dipasang terpisah dari API publik.
func (p *Platform) MetricsHandler() http.Handler { return p.metrics.Handler() }

// RunEmbeddedWorker menjalankan worker M2 di dalam proses server, untuk pengembangan lokal.
func (p *Platform) RunEmbeddedWorker(ctx context.Context) {
	if p.m2Worker != nil {
		p.m2Worker.Run(ctx)
	}
}

func (p *Platform) Close() {
	if p.targets != nil {
		p.targets.Close()
	}
	if p.pool != nil {
		p.pool.Close()
	}
}
