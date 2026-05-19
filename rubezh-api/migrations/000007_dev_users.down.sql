-- Откат миграции 000007: удаление dev-пользователей.
DELETE FROM users
WHERE username IN (SELECT 'dev_' || code FROM roles);
