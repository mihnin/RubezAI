# Итерация J — Чат с документами и контролируемым выводом ПДн за контур

Статус: **черновик на ревью архитектора**. Цель — спроектировать end-to-end
сценарий: в чате можно ввести текст ИЛИ приложить документ с ПДн; перед
отправкой в облачную LLM пользователь видит обезличенный предпросмотр и
статистику и подтверждает отправку; ответ LLM приходит с псевдонимами, а
реальные значения раскрываются по кнопке (детерминированно, аудируется).

## Принятые решения (владелец, 2026-05-22)

1. **Восстановление — детерминированное** (расшифровка `pseudonym_mappings` +
   подстановка строкой). LLM для восстановления **не используется** (известные
   значения нельзя гонять через генеративную модель — риск галлюцинаций).
2. **Триггер восстановления — по кнопке.** Ответ показывается с псевдонимами;
   кнопка «Показать реальные данные» раскрывает по запросу, действие
   аудируется (`response_revealed`).
3. **Перед отправкой в облако — предпросмотр + подтверждение.** Показываем
   обезличенный текст + статистику; кнопка «Отправить в облако». Для
   `trusted_local` гейт не нужен (raw остаётся в периметре).
4. **Загрузка документа — прямо в чате** (attach к сообщению); документ
   парсится, обезличивается и автоматически попадает в раздел «Документы».

## Что уже есть (строим на этом, не дублируем)

- **Оркестратор чата** (`internal/chat/orchestrator.go`):
  `Prepare` (sanitize → карта псевдонимов → policy → Tx1 `chat_request` +
  шифрованные `pseudonym_mappings`) и `Stream` (meta → LLM → leak-check →
  Tx2 `chat_response` → SSE delta → done). Уже разделён на подготовку и стрим.
- **Детерминированное восстановление**: `PseudonymMap.Restore` (in-memory) и
  санитайзерный `restore()` — подстановка псевдоним→raw за один проход.
  Сейчас `finalTexts` при `allow_masked` авто-восстанавливает (`act.restore`).
- **Шифрованные mappings**: таблица `pseudonym_mappings` (raw — AES-256-GCM,
  AAD=SHA-256(session_id‖pseudonym)); пишется в Tx1.
- **Документы**: `rubezh-worker` парсит PDF/DOCX, chunking, БД-очередь
  (`FOR UPDATE SKIP LOCKED`), MinIO; `POST/GET /api/documents`, статусы.
- **Sanitizer**: `/sanitize/preview` возвращает `sanitized_text`, `entities`
  (тип, псевдоним, raw_hash, detector, confidence), `risk` (level, classes).
  Фильтр 1 (regex) + фильтр 2 (локальная LLM, H.3, fail-open).
- **Чат-UI**: SSE-клиент, picker провайдер/модель, lazy-сессия.

## Целевой флоу (cloud-модель)

```
[ввод текста ИЛИ attach документа]
        │
        ▼
1. POST /api/chat/preview            (НЕ вызывает LLM)
   sanitize(text|doc) → masked_text + entities + risk + stats
        │
        ▼
2. UI: «Данные выйдут за контур. Проверьте обезличенный текст.»
   показывает masked_text + статистику (N сущностей по типам, классы риска)
   [Отправить в облако]   [Отмена]
        │ (подтверждение)
        ▼
3. POST /api/chat  (как сейчас, но streamed = masked, БЕЗ авто-restore)
   meta(decision) → LLM(masked) → leak-check → Tx2 → SSE(ответ с псевдонимами)
        │
        ▼
4. UI: ответ с псевдонимами (ФИО_001, КАРТА_002…)
   [Показать реальные данные]
        │ (по кнопке)
        ▼
5. POST /api/chat/messages/{id}/reveal
   читает pseudonym_mappings(session) → расшифровка → подстановка
   → возвращает восстановленный текст; пишет audit `response_revealed`
```

Для `trusted_local` шаг 2 (гейт) пропускается — данные не покидают периметр,
ответ можно сразу показывать в raw.

## Ревью архитектора (7.5/10 → доработка). Ключевые правки ниже учтены.

**Главный блокер (MAJOR-1):** два независимых `sanitize` (на preview и на
chat) НЕ детерминированы — фильтр 2 (LLM-review) fail-open и недетерминирован
(на preview модель нашла сущность, на chat — таймаут → сдвиг счётчика
псевдонимов). Пользователь подтвердил бы один masked-текст, а ушёл бы другой.
**Решение: единый sanitize + `preview_token`** (см. J.0). Остальные MAJOR —
reveal (RBAC/цепочка/AAD/no-store), audit-enum, ре-маскирование — учтены.

## Изменения backend

### J.0 Единый sanitize + `preview_token` (закрывает MAJOR-1)

Sanitize выполняется **один раз** — на preview; результат кэшируется
server-side и переиспользуется в `/api/chat`. Так гарантируется «подтверждён
ровно тот текст, что уйдёт».

- `POST /api/chat/preview` (J.1) кладёт результат (`sanitized_text`,
  `entities`, `risk`, готовый `PseudonymMap` с raw в памяти) в кэш с TTL
  (напр. 10 мин), привязкой к `user_id`+`session_id`, и возвращает непрозрачный
  `preview_token`. Кэш — **только RAM**, не персистится, не логируется
  (redaction как у `PseudonymMap.LogValue`).
- `POST /api/chat` принимает `preview_token` (для cloud-флоу) ИЛИ `text`
  (для trusted_local/без гейта). При наличии токена оркестратор **пропускает**
  `sanitizer.Preview` и строит `Prepared` из кэша (sanitize+pmap уже готовы);
  Tx1 (`chat_request` + шифрованные mappings) пишется здесь, не на preview.
  Токен одноразовый (consume), чужой токен → 403.
- Рефактор: вынести из `Prepare()` шаг «sanitize+pmap» так, чтобы его результат
  кэшировался; `PrepareFromPreview(token)` строит `Prepared` без повторного
  sanitize.

### J.1 Предпросмотр без LLM — `POST /api/chat/preview`

Принимает `{text, document_id?}`, выполняет только `sanitizer.Preview`
(фильтр 1+2), возвращает `{preview_token, sanitized_text, entities, risk}`
без вызова LLM и **без Tx1**. `stats` фронт считает из `entities` (не дублируем
в контракте — MINOR-4). К `entities` применяется тот же whitelist, что в
`history.go`/`chat.go` (start/end и raw не утекают). При `document_id` —
проверка `CheckDocumentAccess(doc, user, role)` (MINOR-2, anti-ACL-bypass).

### J.2 Стрим с псевдонимами + ручное восстановление (reveal)

- **Ре-маскирование (MAJOR-6):** для `allow_masked` ввести в `action` поле
  `revealMode`; `finalTexts` возвращает `stored = streamed = Remask(rawOutput)`
  (а не `stored=rawOutput`) — так и аудит, и показ содержат **только**
  псевдонимы даже при протечке LLM. `DetectLeak(resp.Content)` остаётся ДО
  `finalTexts` (порядок верный). `allow_raw` (trusted_local) — без изменений
  (сразу raw, гейт и reveal не нужны); `summary` — как сейчас (`Remask`).
- **`POST /api/chat/messages/{id}/reveal`** (id = сообщение **ассистента**):
  1. цепочка mappings: `assistant_message.request_id` → парное user-сообщение
     с тем же `request_id` → его `sanitization_result_id` → `pseudonym_mappings`
     (MAJOR-4; reveal на ответе, а mappings привязаны к user-вводу);
  2. AAD расшифровки = `MappingAAD(session_id, pseudonym)`, `session_id` — из
     сессии сообщения; round-trip encrypt→decrypt покрыть тестом;
  3. псевдоним без mapping (LLM перефразировал) — оставить как есть (fail-closed);
  4. возвращает `{revealed_text}`, заголовок **`Cache-Control: no-store`**;
  5. **никогда** не логировать `revealed_text`/raw (negative-тест «raw не в
     логах»).
- **RBAC/policy (MAJOR-2):** reveal разрешён **владельцу сессии** (он сам ввёл
  raw); решение «можно ли reveal» проходит через policy-engine (принцип
  «allow/deny — только policy»), не зашито в роль. Проверка принадлежности
  сообщения сессии и сессии пользователю (как `listChatMessagesHandler`).
  Rate-limit на reveal (anti-bulk-exfiltration).
- Audit-событие `response_revealed` (кто, когда, message_id) — раскрытие raw
  обязано журналироваться.
- Инвариант: raw уходит клиенту **только** через reveal, по явному действию
  владельца, с аудитом и `no-store`.

### J.3 Документ в чате

- `POST /api/chat` (и `/preview`) принимает `document_id` (опц.). Если задан —
  оркестратор берёт извлечённый текст документа (worker уже распарсил) как
  `Message`. Attach в чате = `POST /api/documents` (как сейчас) → polling
  статуса `done` → подстановка `document_id` в запрос.
- Раздел «Документы» дополняется: **скачать оригинал** (есть), **скачать
  обезличенную версию** (новый endpoint: применяет `sanitized_text`/маппинг к
  документу — для MVP: текстовая обезличенная версия), **статистика** по
  документу (кол-во сущностей по типам, классы риска).
- Запись `pseudonym_mappings` уже привязана к сессии; для документа —
  привязка к `document_id` (история «по документу» = список сущностей +
  ссылка на номер документа).

### J.4 Контракты

Новые/изменённые DTO → синхронизировать Go↔Zod (G.1 golden):
`chatPreviewDTO` (preview_token, sanitized_text, entities, risk), `revealDTO`
(revealed_text), расширение `documentDTO` (entity-статистика). Обновить
`docs/contracts/*` + golden (`contract_export_test.go`).
**Audit enum (MAJOR-5):** добавить в `docs/contracts/audit.schema.json`
`$defs.AuditEventType` события `response_revealed` и
`document_masked_downloaded` — иначе ломается контракт-инвариант и фронтовое
отображение. Покрыть контракт-тестом.

## Изменения frontend

- **ChatPage**: attach-файла к сообщению; при cloud-модели — диалог
  предпросмотра (masked_text + статистика + «Отправить в облако»); сообщения
  ассистента с псевдонимами + кнопка «Показать реальные данные» (по клику —
  `reveal`, замена текста, бейдж «раскрыто»). Индикатор «Обрабатываем ПДн…».
- **DocumentsPage**: колонки статистики; кнопки «Оригинал» и «Обезличенный»;
  переход к маппингам/истории по документу.
- Picker: явное разделение «Облачные» (external) / «Локальные»
  (trusted_local) с предупреждением для облачных («данные выходят за контур»).

## Безопасность (инварианты)

- Облачная LLM получает **только** masked text (как сейчас); гейт
  подтверждения делает это видимым пользователю.
- Raw раскрывается только через `reveal`, по явному действию, с аудитом
  `response_revealed`. По умолчанию ответ хранит/показывает псевдонимы.
- Восстановление — детерминированное; LLM в этом пути не участвует.
- `pseudonym_mappings` остаётся единственным источником raw (шифрован).

## Фазы (TDD, отдельные коммиты) — порядок учитывает зависимости из ревью

0. **J.0** Единый sanitize + `preview_token` (RAM-кэш, owner-bound, TTL,
   consume, redaction). База для гейта — делается первой.
1. **J.1** `/api/chat/preview` (возвращает token + masked + entities + risk).
2. **J.2** ре-маскирование (`finalTexts`/`revealMode`) + `/reveal`
   (цепочка message→mappings, AAD, no-store, RBAC/policy, rate-limit,
   audit `response_revealed`, negative-тест «raw не в логах»).
3. **J.3** `document_id` в чате (+ ACL-проверка, лимит токенов/truncate) +
   обезличенная выгрузка (`text/plain` склейка sanitized-чанков, MINOR-1) +
   статистика документа.
4. **J.4** контракты Go↔Zod + `audit.schema.json` enum + frontend
   (диалог-гейт, кнопка reveal, attach в чате, разделение cloud/local в picker'е).
5. UX-экраны (Stitch) параллельно J.1–J.4.

### Обязательные тесты (из ревью)
- preview_token гарантирует идентичность masked-текста preview↔chat;
- round-trip reveal encrypt→decrypt с правильным AAD (session_id);
- negative: `revealed_text`/raw не попадает в application logs;
- leak→`Remask` для `allow_masked` (audit и показ — только псевдонимы);
- contract-тест на новый audit enum.

### Техдолг (явно вынесено)
- Полноценный masked-DOCX (с форматированием) — после MVP; пока `text/plain`.
- Стратегия очень больших документов (> лимита токенов) — truncate + предупреждение.

## История ревью
- v1 — архитектор (subagent Plan), **7.5/10**: MAJOR-1 (детерминизм
  preview↔chat), MAJOR-2/3/4 (reveal RBAC/цепочка/AAD/no-store/redaction),
  MAJOR-5 (audit enum), MAJOR-6 (ре-маскирование). v2 (этот документ) все
  MAJOR закрыл проектно; MINOR учтены или вынесены в техдолг.
