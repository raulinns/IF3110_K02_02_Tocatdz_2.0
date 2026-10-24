package bootstrap

import (
	"context"
	"io"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/argon2"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/admin"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/config"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/explorer"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/postgres"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/ui"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/web"
)

// Platform berisi handler HTTP siap pakai beserta sumber daya yang harus ditutup saat aplikasi berhenti.
type Platform struct {
	Handler http.Handler
	worker  *migration.ReconcileWorker
	targets *postgres.Targets
	pool    *pgxpool.Pool
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
	p.targets, err = postgres.NewTargets(p.pool, box)
	if err != nil {
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
	migrations, err := migration.NewService(migration.Config{
		Repository: ledger, Coordinator: p.targets, Validator: migration.NewPostgreSQLValidator(),
		Authorizer: authorizer, Accounts: store, IDs: tokens, Clock: clock,
	})
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
	adminStore, err := postgres.NewAdminStore(p.pool, box, clock)
	if err != nil {
		return err
	}
	adminService, err := admin.NewService(admin.Config{Repository: adminStore, Authorizer: authorizer, Passwords: hasher, IDs: tokens, Checker: p.targets})
	if err != nil {
		return err
	}
	adminPages, err := ui.NewAdminAdapter(adminService)
	if err != nil {
		return err
	}
	frontend, err := ui.NewFrontend(ui.FrontendConfig{
		Security:   security.HTTPMiddlewareConfig{Auth: auth, Logout: auth, CSRF: csrf, Cookies: cookies, Throttle: limiter},
		Viewer:     viewer,
		Authorizer: authorizer,
		Migrations: migrations,
		Admin:      adminPages,
	})
	if err != nil {
		return err
	}
	p.Handler = web.NewRouter(logger, frontend)
	return nil
}

// RunWorker menjalankan rekonsiliasi migration pending sampai ctx dibatalkan.
func (p *Platform) RunWorker(ctx context.Context) {
	if p.worker != nil {
		p.worker.Run(ctx)
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
