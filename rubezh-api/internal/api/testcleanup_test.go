package api

import (
	"os"
	"testing"

	"github.com/rubezh-ai/rubezh-api/internal/testdb"
)

// TestMain прогоняет тесты пакета api и затем чистит артефакты в общей
// dev-БД, если задан TEST_DATABASE_URL. HTTP-тесты создают политики и
// провайдеров через handler'ы поверх той же БД, что и storage-тесты.
// См. internal/testdb для семантики cleanup'а.
func TestMain(m *testing.M) {
	code := m.Run()
	testdb.Cleanup(os.Getenv("TEST_DATABASE_URL"), testdb.Prefixes())
	os.Exit(code)
}
