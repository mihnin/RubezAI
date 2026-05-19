-- Итерация 1 — миграция 000002: роли и пользователи.

CREATE TABLE roles (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  code        text NOT NULL UNIQUE,
  title       text NOT NULL,
  description text,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now()
);

-- Пользователи не удаляются физически (используется is_active) — поэтому
-- FK на users(id) из аудита и документов используют ON DELETE NO ACTION.
CREATE TABLE users (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  username   text NOT NULL UNIQUE,
  email      text,
  full_name  text,
  role_id    uuid NOT NULL REFERENCES roles(id),
  is_active  boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_users_role ON users(role_id);
-- email уникален среди заполненных значений (NULL допускается до интеграции OIDC).
CREATE UNIQUE INDEX idx_users_email ON users(email) WHERE email IS NOT NULL;

CREATE TRIGGER roles_set_updated_at BEFORE UPDATE ON roles
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER users_set_updated_at BEFORE UPDATE ON users
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Базовые роли (источник истины — docs/ARCHITECTURE.md §11).
INSERT INTO roles (code, title, description) VALUES
  ('user',               'Сотрудник',         'Обычный пользователь чата'),
  ('security_officer',   'Офицер ИБ',          'Контроль безопасности и инциденты'),
  ('compliance_officer', 'Комплаенс / юрист',  'Контроль соответствия и ПДн'),
  ('admin',              'Администратор',      'Управление моделями и политиками'),
  ('auditor',            'Аудитор',            'Просмотр журнала аудита'),
  ('developer',          'Разработчик',        'Модуль «Рубеж Код» (будущее)');
