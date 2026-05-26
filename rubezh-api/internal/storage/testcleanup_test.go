package storage

import (
	"os"
	"testing"

	"github.com/rubezh-ai/rubezh-api/internal/testdb"
)

// TestMain прогоняет тесты пакета и затем чистит артефакты в общей
// dev-БД, если задан TEST_DATABASE_URL. CI чист сам по себе
// (эфемерный postgres), локально — общая compose-БД.
// См. internal/testdb для семантики cleanup'а.
func TestMain(m *testing.M) {
	code := m.Run()
	testdb.Cleanup(os.Getenv("TEST_DATABASE_URL"), testdb.Prefixes())
	os.Exit(code)
}
