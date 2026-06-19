# sentgraph-mcp

Go MCP-сервер долговременной памяти для кодинг-агентов на базе Zep Cloud.

Sentgraph держит локальный слой тонким:

- Zep Cloud отвечает за построение графа, дедупликацию, эмбеддинги и извлечение.
- Sentgraph отдаёт агенту шесть базовых MCP-инструментов.
- Нативные Go-хуки часто читают и пишут память без локального демона.
- Память проекта разделена по `project_id`, поэтому один проект может охватывать несколько репозиториев.

## Установка

Без клонирования репозитория (Go скачает модуль сам):

```bash
go install github.com/shilin23061991/sentgraph-mcp@latest
```

Из клонированного репозитория:

```bash
go install .
```

Бинарь `sentgraph-mcp` попадёт в `$(go env GOBIN)` (или `$(go env GOPATH)/bin`) -- убедитесь, что этот каталог в `PATH`.

## Установка в Claude Code

Шаги ниже подключают MCP-сервер, ставят скилы и хуки и дописывают промт. Все команды выполняйте из корня репозитория.

### 1. Подключить MCP-сервер

stdio (по умолчанию для Claude Code). Скоуп проекта: конфиг пишется в `.mcp.json` в корне репозитория (коммитится, шарится с командой):

```bash
claude mcp add --transport stdio \
  --env ZEP_API_KEY='${ZEP_API_KEY}' \
  --env ZEP_USER_ID='${ZEP_USER_ID}' \
  --env SENTGRAPH_PROJECT_ID="sentoke" \
  --scope project sentgraph -- sentgraph-mcp serve
```

> Скоуп `project` пишет конфиг в `.mcp.json` (его коммитят). Секреты туда не попадают: `ZEP_API_KEY` и `ZEP_USER_ID` записываются литералами `${...}` (одинарные кавычки -- shell их не раскрывает) и подставляются из окружения каждого разработчика при старте сессии; коммитится только `SENTGRAPH_PROJECT_ID` (замените `sentoke` на свой id). Если `${ZEP_API_KEY}`/`${ZEP_USER_ID}` не заданы в окружении -- Claude Code не запустит сервер.
>
> Перед первым использованием project-сервер требует подтверждения (`claude mcp list` покажет `Pending approval`). `--scope` стоит между последним `--env` и именем `sentgraph` намеренно -- иначе CLI читает имя как пару `KEY=value`; `--` отделяет команду запуска.

Вариант с HTTP (сервер в отдельном процессе):

```bash
sentgraph-mcp serve --http :8080                               # терминал 1
claude mcp add --transport http --scope project sentgraph http://localhost:8080   # терминал 2
```

Проверка:

```bash
claude mcp list
claude mcp get sentgraph
```

### 3. Установить скилы

Личные (для всех проектов):

```bash
mkdir -p ~/.claude/skills
cp -R plugin/skills/* ~/.claude/skills/
```

Либо в рамках проекта (коммитятся в репозиторий):

```bash
mkdir -p .claude/skills
cp -R plugin/skills/* .claude/skills/
```

Станут доступны команды `/remember`, `/recall`, `/forget`, `/session-history`, `/sentgraph-tools`.

### 4. Установить хуки

Хуки объявляются в `settings.json`. Команда ниже вмердживает блок `hooks` из `plugin/hooks/hooks.json` в пользовательские настройки (нужен `jq`):

```bash
mkdir -p ~/.claude
[ -f ~/.claude/settings.json ] || echo '{}' > ~/.claude/settings.json
tmp="$(mktemp)"
jq --slurpfile h plugin/hooks/hooks.json '.hooks = ((.hooks // {}) * $h[0].hooks)' \
  ~/.claude/settings.json > "$tmp" && mv "$tmp" ~/.claude/settings.json
```

> Команда идемпотентна, но заменяет массивы хуков для событий `SessionStart`, `UserPromptSubmit`, `PreCompact`, `Stop`, `SessionEnd`. Если на этих событиях уже висят ваши хуки, объедините блоки вручную. Для настроек проекта используйте `.claude/settings.json`.

### 5. Дописать промт

Добавьте блок памяти в файл памяти Claude (один раз):

```bash
# либо для текущего проекта
printf '\n' >> ./CLAUDE.md && cat CLAUDE.snippet.md >> ./CLAUDE.md
```

### Финальная проверка

```bash
sentgraph-mcp doctor --online   # проверка конфигурации + связи с Zep
claude mcp list                 # sentgraph должен быть в списке
```

## Команды

```bash
sentgraph-mcp doctor             # проверка конфигурации (API-ключ, пользователь, project id)
sentgraph-mcp doctor --online    # дополнительно проверить связь с Zep (user/project graph/thread)
sentgraph-mcp serve              # MCP по stdio (по умолчанию для Claude Code / Cursor)
sentgraph-mcp serve --http :8080 # MCP по Streamable HTTP на ADDR
sentgraph-mcp hook SessionStart
```

## MCP-инструменты

- `memory_context` -- собранный контекст пользователя и проекта.
- `memory_search` -- поиск по графовой памяти пользователя или проекта.
- `memory_history` -- недавние сообщения треда.
- `memory_add_messages` -- сохранить ходы беседы.
- `memory_add` -- сохранить факты или данные проекта/пользователя.
- `memory_forget` -- удалить edge, node или episode по UUID.

## Хуки

Конфигурация хуков плагина Claude -- в `plugin/hooks/hooks.json`.

События по умолчанию:

- `SessionStart` -- прочитать контекст и подгрузить его.
- `UserPromptSubmit` -- записать промт пользователя и подгрузить свежий контекст.
- `PreCompact` -- повторно подгрузить контекст перед компакцией.
- `Stop` -- сохранить последний ход ассистента из транскрипта.
- `SessionEnd` -- финальный проход сохранения.
