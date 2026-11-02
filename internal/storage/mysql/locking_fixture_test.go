package mysql_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"os"
	"strings"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

// Rewrite only test SQL to unique table names. This lets real repository queries
// run over multiple connections without touching existing application tables.
type isolatedTablesDriver struct{ replace *strings.Replacer }

func (d isolatedTablesDriver) Open(dsn string) (driver.Conn, error) {
	conn, err := (&mysqldriver.MySQLDriver{}).Open(dsn)
	if err != nil {
		return nil, err
	}
	return &isolatedTablesConn{Conn: conn, replace: d.replace}, nil
}

type isolatedTablesConn struct {
	driver.Conn
	replace *strings.Replacer
}

func (c *isolatedTablesConn) Prepare(query string) (driver.Stmt, error) {
	return c.Conn.Prepare(c.replace.Replace(query))
}

func (c *isolatedTablesConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	return c.Conn.(driver.ConnBeginTx).BeginTx(ctx, opts)
}

func (c *isolatedTablesConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if exec, ok := c.Conn.(driver.ExecerContext); ok {
		return exec.ExecContext(ctx, c.replace.Replace(query), args)
	}
	return nil, driver.ErrSkip
}

func (c *isolatedTablesConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if queryer, ok := c.Conn.(driver.QueryerContext); ok {
		return queryer.QueryContext(ctx, c.replace.Replace(query), args)
	}
	return nil, driver.ErrSkip
}

func newLockingTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	dsn := os.Getenv("MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("set MYSQL_TEST_DSN to run MySQL locking tests")
	}
	cfg, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid MYSQL_TEST_DSN")
	}
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	cfg.Timeout = 5 * time.Second
	cfg.ReadTimeout = 5 * time.Second
	cfg.WriteTimeout = 5 * time.Second
	prefix := "test_" + strings.ReplaceAll(uuid.NewString(), "-", "") + "_"
	replace := strings.NewReplacer("team_members", prefix+"team_members", "teams", prefix+"teams", "users", prefix+"users")
	driverName := prefix + "driver"
	sql.Register(driverName, isolatedTablesDriver{replace: replace})
	db, err := sqlx.Open(driverName, cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(4)
	var created []string
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for i := len(created) - 1; i >= 0; i-- {
			if _, err := db.ExecContext(ctx, "DROP TABLE "+created[i]); err != nil {
				t.Errorf("drop isolated table: %v", err)
			}
		}
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, migration := range []struct{ file, table string }{
		{"000001_create_users_table.up.sql", "users"},
		{"000002_teams.up.sql", "teams"},
		{"000003_team_members.up.sql", "team_members"},
	} {
		schema, err := os.ReadFile("../../../migrations/" + migration.file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, string(schema)); err != nil {
			t.Fatalf("create isolated %s: %v", migration.table, err)
		}
		created = append(created, migration.table)
	}
	return db
}
