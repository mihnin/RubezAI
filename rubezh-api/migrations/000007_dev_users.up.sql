-- Итерация 8 — миграция 000007: dev-пользователи (по одному на роль).
-- MVP-механизм идентичности: dev-токен несёт только роль, user_id
-- резолвится по роли (storage.UserIDForRole). Заменяется реальными
-- пользователями с приходом OIDC. Идемпотентно (ON CONFLICT DO NOTHING).
INSERT INTO users (username, full_name, role_id)
SELECT 'dev_' || r.code, 'Dev: ' || r.title, r.id
FROM roles r
ON CONFLICT (username) DO NOTHING;
