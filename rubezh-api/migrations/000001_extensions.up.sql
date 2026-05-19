-- Итерация 1 — миграция 000001: расширения и общие триггерные функции.

CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Автообновление updated_at при UPDATE строки.
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  NEW.updated_at := now();
  RETURN NEW;
END;
$$;

-- Запрет мутаций для append-only таблиц (audit_events).
CREATE OR REPLACE FUNCTION rubezh_block_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'append-only нарушение: операция % запрещена на таблице %',
    TG_OP, TG_TABLE_NAME USING ERRCODE = 'check_violation';
END;
$$;
