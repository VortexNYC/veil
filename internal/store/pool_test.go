package store

import (
	"os"
	"testing"
)

// TestPostgresPoolBudget proves the connection-budget knobs actually bind:
// VEIL_PG_MAX_CONNS resizes the request pool, VEIL_PG_AUDIT_CONNS the audit
// pool, and an explicit DSN pool_max_conns is respected when env is unset.
func TestPostgresPoolBudget(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	t.Setenv("VEIL_PG_MAX_CONNS", "7")
	t.Setenv("VEIL_PG_AUDIT_CONNS", "1")
	s, err := OpenPostgres(dsn, testMasterKey(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got := s.pool.Stat().MaxConns(); got != 7 {
		t.Fatalf("main pool MaxConns %d, want 7", got)
	}
	if got := s.auditPool.Stat().MaxConns(); got != 1 {
		t.Fatalf("audit pool MaxConns %d, want 1", got)
	}

	// Env wins over DSN.
	s2, err := OpenPostgres(dsn+"&pool_max_conns=3", testMasterKey(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if got := s2.pool.Stat().MaxConns(); got != 7 {
		t.Fatalf("env did not win: MaxConns %d, want 7", got)
	}

	// DSN respected when env unset.
	t.Setenv("VEIL_PG_MAX_CONNS", "")
	s3, err := OpenPostgres(dsn+"&pool_max_conns=3", testMasterKey(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s3.Close()
	if got := s3.pool.Stat().MaxConns(); got != 3 {
		t.Fatalf("DSN pool_max_conns ignored: MaxConns %d, want 3", got)
	}

	// Default when neither is set.
	s4, err := OpenPostgres(dsn, testMasterKey(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s4.Close()
	if got := s4.pool.Stat().MaxConns(); got != 20 {
		t.Fatalf("default MaxConns %d, want 20", got)
	}
}
