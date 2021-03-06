package physical

import (
	"database/sql"
	"fmt"
	"os"
	"testing"

	dockertest "gopkg.in/ory-am/dockertest.v3"

	"github.com/hashicorp/vault/helper/logformat"
	log "github.com/mgutz/logxi/v1"

	_ "github.com/lib/pq"
)

func prepareCockroachDBTestContainer(t *testing.T) (cleanup func(), retURL, tableName string) {
	tableName = os.Getenv("CR_TABLE")
	if tableName == "" {
		tableName = "vault_kv_store"
	}
	retURL = os.Getenv("CR_URL")
	if retURL != "" {
		return func() {}, retURL, tableName
	}

	pool, err := dockertest.NewPool("")
	if err != nil {
		t.Fatalf("Failed to connect to docker: %s", err)
	}

	dockerOptions := &dockertest.RunOptions{
		Repository: "cockroachdb/cockroach",
		Tag:        "release-1.0",
		Cmd:        []string{"start", "--insecure"},
	}
	resource, err := pool.RunWithOptions(dockerOptions)
	if err != nil {
		t.Fatalf("Could not start local CockroachDB docker container: %s", err)
	}

	cleanup = func() {
		err := pool.Purge(resource)
		if err != nil {
			t.Fatalf("Failed to cleanup local container: %s", err)
		}
	}

	retURL = fmt.Sprintf("postgresql://root@localhost:%s/?sslmode=disable", resource.GetPort("26257/tcp"))
	database := "database"
	tableName = database + ".vault_kv"

	// exponential backoff-retry
	if err = pool.Retry(func() error {
		var err error
		db, err := sql.Open("postgres", retURL)
		if err != nil {
			return err
		}
		_, err = db.Exec("CREATE DATABASE database")
		return err
	}); err != nil {
		cleanup()
		t.Fatalf("Could not connect to docker: %s", err)
	}
	return cleanup, retURL, tableName
}

func TestCockroachDBBackend(t *testing.T) {
	cleanup, connURL, table := prepareCockroachDBTestContainer(t)
	defer cleanup()

	// Run vault tests
	logger := logformat.NewVaultLogger(log.LevelTrace)

	b, err := NewBackend("cockroachdb", logger, map[string]string{
		"connection_url": connURL,
		"table":          table,
	})

	if err != nil {
		t.Fatalf("Failed to create new backend: %v", err)
	}

	defer func() {
		truncate(t, b)
	}()

	testBackend(t, b)
	truncate(t, b)
	testBackend_ListPrefix(t, b)
	truncate(t, b)
	testTransactionalBackend(t, b)
}

func truncate(t *testing.T, b Backend) {
	crdb := b.(*CockroachDBBackend)
	_, err := crdb.client.Exec("TRUNCATE TABLE " + crdb.table)
	if err != nil {
		t.Fatalf("Failed to drop table: %v", err)
	}
}
