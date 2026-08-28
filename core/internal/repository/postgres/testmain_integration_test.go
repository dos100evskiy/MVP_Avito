//go:build integration

// Интеграционные тесты репозиториев поднимают настоящий PostgreSQL в Docker
// через testcontainers-go и гоняют реальные SQL-запросы репозиториев поверх
// него — в отличие от юнит-тестов usecase на моках (internal/usecase/*_test.go),
// это единственный уровень, где ловятся баги вроде нарушения foreign key,
// синтаксических ошибок SQL или несовпадения типов колонок. Именно такой
// баг (запись order_status_history мимо открытой транзакции создания заказа)
// был найден и исправлен вручную при первом реальном прогоне через curl —
// TestOrderRepo_Create_AppendsStatusHistoryInSameTransaction ниже как раз
// закрывает эту дыру автоматической проверкой.
//
// Тесты помечены билд-тегом "integration" и НЕ запускаются обычным
// `go test ./...` / `make test` — им нужен Docker. Запуск:
//
//	make test-integration
//
// или вручную:
//
//	go test -tags=integration ./internal/repository/postgres/... -v
package postgres_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	tc "github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/avito/kuhnya/core/internal/repository/postgres"
)

// testPool — общий пул соединений на весь тестовый бинарник этого пакета.
// Поднимать контейнер на каждый Test* было бы правильнее с точки зрения
// изоляции, но заметно медленнее (контейнер стартует секунды); вместо этого
// каждый тест сам создаёт себе уникальные фикстуры (venue/items со случайными
// UUID через insertVenue/CreateItem), поэтому тесты друг другу не мешают,
// несмотря на общую БД.
var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	ctx := context.Background()

	container, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("avito_kuhnya_test"),
		tcpostgres.WithUsername("avito"),
		tcpostgres.WithPassword("avito"),
		tc.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, "testcontainers: failed to start postgres:", err)
		os.Exit(1)
	}
	defer func() {
		if err := container.Terminate(context.Background()); err != nil {
			fmt.Fprintln(os.Stderr, "testcontainers: failed to terminate postgres:", err)
		}
	}()

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		fmt.Fprintln(os.Stderr, "testcontainers: failed to get connection string:", err)
		os.Exit(1)
	}

	pool, err := postgres.NewPool(ctx, dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to connect to test db:", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := applyMigrations(ctx, pool); err != nil {
		fmt.Fprintln(os.Stderr, "failed to apply migrations:", err)
		os.Exit(1)
	}

	testPool = pool
	os.Exit(m.Run())
}

// applyMigrations читает core/migrations/*.up.sql в порядке имён файлов и
// выполняет каждый файл как набор стейтментов, разделённых ";". Без
// golang-migrate — чтобы не тащить ещё одну зависимость только ради тестов;
// путь к БД в тестах и так временный (одноразовый контейнер), поэтому нам
// не нужна умная система версионирования/отката, только "накатить всё по порядку".
func applyMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	// Путь считается от пакета internal/repository/postgres до core/migrations.
	dir := filepath.Join("..", "..", "..", "migrations")

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read migrations dir %s: %w", dir, err)
	}

	var files []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".up.sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files) // "000001_..." < "000002_..." — лексикографический порядок совпадает с числовым

	for _, f := range files {
		content, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			return fmt.Errorf("read migration %s: %w", f, err)
		}
		for _, stmt := range strings.Split(string(content), ";") {
			stmt = strings.TrimSpace(stmt)
			if stmt == "" {
				continue
			}
			// Ведущие "--"-комментарии внутри стейтмента (например, перед
			// CREATE EXTENSION) не мешают — Postgres сам их игнорирует как
			// часть той же команды, экранировать/вырезать не нужно.
			if _, err := pool.Exec(ctx, stmt); err != nil {
				return fmt.Errorf("apply migration %s, statement %q: %w", f, stmt, err)
			}
		}
	}
	return nil
}
