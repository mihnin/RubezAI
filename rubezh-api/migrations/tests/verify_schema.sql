-- Проверка схемы БД «Рубеж ИИ» (Итерация 1).
-- Запуск: psql -v ON_ERROR_STOP=1 -f verify_schema.sql
--
-- До применения миграций скрипт падает — это TDD-«красный» тест.
-- После применения всех миграций — печатает SCHEMA VERIFICATION PASSED.
-- Скрипт не оставляет следов в БД (всё внутри транзакции с ROLLBACK).

\set ON_ERROR_STOP on
BEGIN;

-- 1. Расширения
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'vector') THEN
    RAISE EXCEPTION 'Расширение pgvector не установлено';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pgcrypto') THEN
    RAISE EXCEPTION 'Расширение pgcrypto не установлено';
  END IF;
  RAISE NOTICE 'OK: расширения vector, pgcrypto';
END $$;

-- 2. Все 14 MVP-таблиц существуют
DO $$
DECLARE
  expected text[] := ARRAY[
    'roles','users','model_providers','policies','policy_versions',
    'documents','document_chunks','embeddings','chat_sessions','chat_messages',
    'sanitization_results','pseudonym_mappings','audit_events','incidents'];
  t text;
BEGIN
  FOREACH t IN ARRAY expected LOOP
    IF NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = 'public' AND table_name = t
    ) THEN
      RAISE EXCEPTION 'Таблица "%" отсутствует', t;
    END IF;
  END LOOP;
  RAISE NOTICE 'OK: все 14 MVP-таблиц на месте';
END $$;

-- 3. created_at у всех таблиц; updated_at у мутабельных
DO $$
DECLARE
  all_tables text[] := ARRAY[
    'roles','users','model_providers','policies','policy_versions',
    'documents','document_chunks','embeddings','chat_sessions','chat_messages',
    'sanitization_results','pseudonym_mappings','audit_events','incidents'];
  with_updated text[] := ARRAY[
    'roles','users','model_providers','policies',
    'documents','chat_sessions','incidents'];
  t text;
BEGIN
  FOREACH t IN ARRAY all_tables LOOP
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns
      WHERE table_schema='public' AND table_name=t AND column_name='created_at') THEN
      RAISE EXCEPTION 'Таблица "%" без created_at', t;
    END IF;
  END LOOP;
  FOREACH t IN ARRAY with_updated LOOP
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns
      WHERE table_schema='public' AND table_name=t AND column_name='updated_at') THEN
      RAISE EXCEPTION 'Мутабельная таблица "%" без updated_at', t;
    END IF;
  END LOOP;
  RAISE NOTICE 'OK: created_at/updated_at согласно конвенции';
END $$;

-- 4. audit_events — append-only: UPDATE и DELETE заблокированы триггером
DO $$
DECLARE
  blocked boolean;
BEGIN
  INSERT INTO audit_events (event_type) VALUES ('__schema_test__');

  blocked := false;
  BEGIN
    UPDATE audit_events SET event_type = 'x' WHERE event_type = '__schema_test__';
  EXCEPTION WHEN OTHERS THEN blocked := true;
  END;
  IF NOT blocked THEN RAISE EXCEPTION 'audit_events: UPDATE не заблокирован'; END IF;

  blocked := false;
  BEGIN
    DELETE FROM audit_events WHERE event_type = '__schema_test__';
  EXCEPTION WHEN OTHERS THEN blocked := true;
  END;
  IF NOT blocked THEN RAISE EXCEPTION 'audit_events: DELETE не заблокирован'; END IF;

  RAISE NOTICE 'OK: audit_events append-only (UPDATE/DELETE заблокированы)';
END $$;

-- 5. pseudonym_mappings хранит зашифрованное значение, а не raw
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM information_schema.columns
    WHERE table_schema='public' AND table_name='pseudonym_mappings'
      AND column_name='raw_value_encrypted' AND data_type='bytea') THEN
    RAISE EXCEPTION 'pseudonym_mappings: нет bytea-колонки raw_value_encrypted';
  END IF;
  IF EXISTS (SELECT 1 FROM information_schema.columns
    WHERE table_schema='public' AND table_name='pseudonym_mappings'
      AND column_name IN ('raw_value','value','plaintext')) THEN
    RAISE EXCEPTION 'pseudonym_mappings: обнаружена raw-колонка — запрещено';
  END IF;
  RAISE NOTICE 'OK: pseudonym_mappings хранит только зашифрованное значение';
END $$;

-- 6. audit_events привязан к неизменяемой версии политики через внешний ключ
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint c
    JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = ANY (c.conkey)
    WHERE c.contype = 'f'
      AND c.conrelid = 'audit_events'::regclass
      AND c.confrelid = 'policy_versions'::regclass
      AND a.attname = 'policy_version_id'
  ) THEN
    RAISE EXCEPTION 'audit_events.policy_version_id не является FK на policy_versions';
  END IF;
  RAISE NOTICE 'OK: audit_events привязан к версии политики (FK)';
END $$;

-- 7. forensics-данные не уничтожаются каскадом (FK с ON DELETE SET NULL)
DO $$
DECLARE
  bad text;
BEGIN
  SELECT string_agg(conname, ', ') INTO bad
  FROM pg_constraint
  WHERE contype = 'f'
    AND conrelid = 'sanitization_results'::regclass
    AND confdeltype <> 'n';
  IF bad IS NOT NULL THEN
    RAISE EXCEPTION 'sanitization_results: каскадные FK уничтожают forensics: %', bad;
  END IF;
  RAISE NOTICE 'OK: sanitization_results защищены от каскадного удаления';
END $$;

ROLLBACK;
\echo '=== SCHEMA VERIFICATION PASSED ==='
