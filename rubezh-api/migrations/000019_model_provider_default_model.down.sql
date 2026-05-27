-- Откат: убрать default_model. Не очищаем значения — DROP COLUMN
-- ликвидирует данные вместе с колонкой.
ALTER TABLE model_providers DROP COLUMN IF EXISTS default_model;
