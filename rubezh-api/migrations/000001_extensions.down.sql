-- Откат миграции 000001.

DROP FUNCTION IF EXISTS rubezh_block_mutation();
DROP FUNCTION IF EXISTS set_updated_at();
DROP EXTENSION IF EXISTS pgcrypto;
DROP EXTENSION IF EXISTS vector;
