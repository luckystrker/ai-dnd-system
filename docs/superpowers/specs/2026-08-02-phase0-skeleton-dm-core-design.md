# Phase 0: Skeleton & DM Core — Design Spec

> Date: 2026-08-02
> Status: Approved
> Scope: Single player text chat with AI Dungeon Master, dice rolling, token streaming.

---

## 1. Overview

Phase 0 delivers the minimal playable vertical slice: one or more players join a room via a short code, type actions in a chat, and receive streamed narrative responses from an AI Dungeon Master that can roll dice and perform skill checks.

**Acceptance criteria:**
1. `docker compose up` → all services healthy
2. Player opens UI → creates room → gets a join code
3. Player types "I try to pick the lock" → DM streams a narrative response
4. DM calls `skill_check` → UI shows dice result inline
5. DM narrates outcome based on roll result
6. Second player joins with room code → sees same chat
7. Reconnect with same token → player recognized

---

## 2. Technology Choices

| Component | Choice | Rationale |
|-----------|--------|-----------|
| Python tooling | uv | Fast, modern, pyproject.toml native |
| Backend | FastAPI + uvicorn | Async-native, WebSocket support |
| Agent orchestration | LangGraph | Stateful graph, explicit control, scales to NPC agents |
| LLM access | langchain-openai `ChatOpenAI` with `base_url` | Any OpenAI-compatible API |
| Default LLM | OpenRouter + deepseek-v4-flash | Configurable via env |
| Realtime | In-process Event Bus (asyncio pub/sub) | Broadcasts to room, evolves into Phase 1 Event Bus |
| Response delivery | Token-by-token streaming via WebSocket | <3s feedback constraint |
| Database | PostgreSQL 16 + pgvector | Single DB for all phases |
| Migrations | Alembic | Standard for SQLAlchemy |
| ORM | SQLAlchemy 2.0 (async) | Mature, typed |
| Frontend | React 18 + TypeScript + Vite | Plan requirement |
| State management | Zustand | Lightweight, minimal boilerplate |
| Styling | Tailwind CSS, dark theme | Fast, fits TTRPG mood |
| Room access | Room code + local player token | No auth, casual, reconnect support |
| Containerization | Docker Compose | Postgres + backend + frontend |

---

## 3. Project Structure

```
ai-dnd-system/
├── docker-compose.yml
├── .env.example
├── backend/
│   ├── pyproject.toml
│   ├── alembic.ini
│   ├── app/
│   │   ├── main.py             # FastAPI app factory, lifespan
│   │   ├── config.py           # pydantic-settings, env vars
│   │   ├── db.py               # async engine, session factory
│   │   ├── models/             # SQLAlchemy ORM models
│   │   │   ├── room.py
│   │   │   ├── player.py
│   │   │   └── character.py
│   │   ├── schemas/            # Pydantic request/response schemas
│   │   │   ├── room.py
│   │   │   └── ws_messages.py
│   │   ├── api/                # REST routes + WebSocket handler
│   │   │   ├── rooms.py
│   │   │   └── ws.py
│   │   ├── agents/             # LangGraph DM agent
│   │   │   ├── dm_agent.py     # graph definition
│   │   │   ├── prompts/
│   │   │   │   └── dm_system.py
│   │   │   └── tools/
│   │   │       ├── dice.py
│   │   │       └── checks.py
│   │   ├── engine/             # deterministic game logic
│   │   │   ├── rules_base.py   # abstract GameSystem
│   │   │   └── systems/
│   │   │       └── dnd5e.py
│   │   └── services/
│   │       ├── event_bus.py    # asyncio pub/sub
│   │       └── room_manager.py # room CRUD, membership
│   └── migrations/
│       └── versions/
├── frontend/
│   ├── package.json
│   ├── vite.config.ts
│   ├── tailwind.config.ts
│   ├── index.html
│   └── src/
│       ├── App.tsx
│       ├── components/
│       │   ├── Chat/
│       │   │   ├── ChatWindow.tsx
│       │   │   ├── MessageList.tsx
│       │   │   ├── MessageInput.tsx
│       │   │   └── DiceIndicator.tsx
│       │   └── Room/
│       │       ├── Lobby.tsx
│       │       └── RoomView.tsx
│       ├── hooks/
│       │   └── useWebSocket.ts
│       ├── stores/
│       │   ├── roomStore.ts
│       │   └── chatStore.ts
│       └── types/
│           └── messages.ts
└── docs/
    ├── DEVELOPMENT_PLAN.md
    └── superpowers/specs/
```

---

## 4. Database Schema

```sql
CREATE TABLE rooms (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code VARCHAR(6) UNIQUE NOT NULL,
    config JSONB DEFAULT '{}',
    status VARCHAR(20) DEFAULT 'active',
    created_at TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE players (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    token VARCHAR(64) UNIQUE NOT NULL,
    display_name VARCHAR(50) NOT NULL
);

CREATE TABLE room_players (
    room_id UUID REFERENCES rooms(id),
    player_id UUID REFERENCES players(id),
    joined_at TIMESTAMPTZ DEFAULT now(),
    PRIMARY KEY (room_id, player_id)
);

CREATE TABLE characters (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    room_id UUID REFERENCES rooms(id),
    player_id UUID REFERENCES players(id),
    name VARCHAR(100) NOT NULL,
    stats JSONB DEFAULT '{}',
    inventory JSONB DEFAULT '[]',
    created_at TIMESTAMPTZ DEFAULT now()
);
```

**Notes:**
- `rooms.code`: 6-char alphanumeric (uppercase, no ambiguous chars like 0/O/1/I)
- `players.token`: 64-char random hex, generated server-side
- `characters.stats`: system-specific. For D&D 5e: `{str, dex, con, int, wis, cha, hp, max_hp, ac, ...}`
- No HP/AC columns — all game-specific data lives in `stats` JSONB (multi-system support)

---

## 5. Event Bus & WebSocket Protocol

### 5.1 Event Bus

In-process asyncio pub/sub. One topic per room.

```python
class EventBus:
    async def publish(self, room_id: str, event: Event) -> None: ...
    def subscribe(self, room_id: str, ws: WebSocket) -> Subscription: ...
    def unsubscribe(self, room_id: str, ws: WebSocket) -> None: ...
```

`Event` model: `{type: str, payload: dict, timestamp: datetime, sender_id: str | None}`

On publish: serialize to JSON → broadcast to all WebSocket subscribers in the room.

### 5.2 WebSocket Endpoint

`ws://host/ws/room/{code}?token={player_token}`

### 5.3 Message Protocol (JSON)

**Client → Server:**
| type | payload | description |
|------|---------|-------------|
| `player_message` | `{text: string}` | Player action/dialogue |
| `ping` | `{}` | Keepalive |

**Server → Client:**
| type | payload | description |
|------|---------|-------------|
| `dm_token` | `{token: string}` | Single streamed token from DM |
| `dm_complete` | `{message_id: string, full_text: string}` | DM finished responding |
| `dice_roll` | `{tool: string, args: dict, result: dict}` | Tool call result (dice/check) |
| `player_joined` | `{name: string}` | Another player connected |
| `player_message` | `{sender: string, text: string}` | Echo of player message to all |
| `error` | `{message: string}` | Error notification |
| `pong` | `{}` | Keepalive response |

### 5.4 Message Flow

1. Player sends `player_message`
2. WS handler publishes `player_message` event → all clients display it
3. WS handler spawns `asyncio.Task` for DM agent
4. DM agent streams via `.astream_events(version="v2")`:
   - `on_chat_model_stream` → publish `dm_token`
   - `on_tool_start` / `on_tool_end` → publish `dice_roll`
5. Graph completes → publish `dm_complete`

### 5.5 Concurrency

DM processes one message at a time per room. If multiple players send messages while the DM is responding, they are queued (asyncio.Queue per room) and processed sequentially. This keeps narrative coherent.

### 5.6 Reconnection

Client reconnects with same token → `GET /room/{code}` restores state → WS re-subscribes. No message history replay in Phase 0 (chat starts fresh on page load; persistence comes in Phase 1).

---

## 6. DM Agent (LangGraph)

### 6.1 Graph Structure

ReAct loop:

```
START → agent_node → should_continue? ─yes→ tool_node → agent_node
                                   └─no→ END
```

- `agent_node`: invokes LLM with messages (system prompt + history + tool results)
- `should_continue`: routes based on presence of `tool_calls` in last AI message
- `tool_node`: executes tool(s), appends `ToolMessage` to state

### 6.2 State

```python
class DMState(MessagesState):
    room_id: str
    character_context: str
```

### 6.3 System Prompt

Stored in `app/agents/prompts/dm_system.py`. Core directives:
- You are a Dungeon Master for a tabletop RPG
- Narrative-first: describe scenes, reactions, consequences vividly
- Use tools when an action has uncertain outcome — never invent dice results
- Keep responses concise (2-4 paragraphs unless the scene demands more)
- Tone: high fantasy, adventurous, casual rules

Dynamic context appended at invocation:
- Character name + stats summary
- (Phase 1+: recent events, relevant memory)

### 6.4 Tools

| Tool | Parameters | Returns |
|------|-----------|---------|
| `roll_dice` | `sides: int, count: int = 1` | `{rolls: [int], total: int}` |
| `skill_check` | `character_name: str, skill: str, difficulty: int` | `{roll, modifier, total, difficulty, success, margin}` |

Both delegate to the Rules Engine. Decorated with `@tool` (langchain).

### 6.5 Streaming

- LangGraph `.astream_events(version="v2")`
- Filter `on_chat_model_stream` → extract `chunk.content` → publish `dm_token`
- Filter `on_tool_end` → publish `dice_roll` with structured result
- On completion → assemble full text → publish `dm_complete`

### 6.6 LLM Configuration

```python
ChatOpenAI(
    base_url=settings.LLM_BASE_URL,   # https://openrouter.ai/api/v1
    api_key=settings.LLM_API_KEY,
    model=settings.LLM_MODEL,          # deepseek-v4-flash
    streaming=True,
)
```

---

## 7. Rules Engine

### 7.1 Architecture

Abstract `GameSystem` interface → concrete `DnD5eSystem`. Phase 0 implements only dice + checks.

```python
class GameSystem(ABC):
    def roll_dice(self, sides: int, count: int) -> DiceResult: ...
    def skill_check(self, stats: dict, skill: str, difficulty: int, advantage: bool | None = None) -> CheckResult: ...
```

### 7.2 D&D 5e Implementation

**Dice:** `random.randint(1, sides)` per die. Returns individual rolls + total.

**Skill check:**
1. Determine ability from skill mapping
2. Compute modifier: `(ability_score - 10) // 2`
3. Roll d20 (advantage: 2d20 take highest; disadvantage: 2d20 take lowest)
4. Total = roll + modifier
5. Success if total ≥ difficulty

**Skill → Ability mapping:**
| Ability | Skills |
|---------|--------|
| STR | Athletics |
| DEX | Acrobatics, Sleight of Hand, Stealth |
| INT | Arcana, History, Investigation, Nature, Religion |
| WIS | Animal Handling, Insight, Medicine, Perception, Survival |
| CHA | Deception, Intimidation, Performance, Persuasion |

**Result models:**
```python
class DiceResult(BaseModel):
    rolls: list[int]
    total: int

class CheckResult(BaseModel):
    roll: int
    modifier: int
    total: int
    difficulty: int
    success: bool
    margin: int
```

---

## 8. REST API

| Method | Path | Body | Response | Description |
|--------|------|------|----------|-------------|
| POST | `/room/create` | `{display_name: str}` | `{room_id, code, player_token}` | Create room + player |
| POST | `/room/join` | `{code: str, display_name: str, player_token?: str}` | `{room_id, player_token}` | Join existing room |
| GET | `/room/{code}` | — | `{room: {...}, players: [...], characters: [...]}` | Room state (reconnect) |
| GET | `/health` | — | `{status: "ok"}` | Health check |

---

## 9. Frontend

### 9.1 Views

1. **Lobby** (`/`): Create or join a room. Display name input + room code field.
2. **Room** (`/room/:code`): Chat interface.

### 9.2 Chat UI

- Message list: player messages (right-aligned or named), DM messages (styled distinctly), dice results (inline badges)
- Streaming: DM tokens appear progressively with a blinking cursor indicator
- Input: text field + send button, Enter to send
- Auto-scroll on new content

### 9.3 State (Zustand)

```typescript
// roomStore
{ roomCode, playerToken, playerName, players: string[], connected: boolean }

// chatStore
{ messages: Message[], streamingText: string, isStreaming: boolean }
```

### 9.4 WebSocket Hook

- Connect on room mount, disconnect on unmount
- Parse events → dispatch to stores
- Auto-reconnect with exponential backoff (3 attempts)
- Ping every 30s

### 9.5 Styling

- Tailwind CSS, dark theme
- Desktop-first, responsive
- No component library

---

## 10. Docker Compose

```yaml
services:
  postgres:
    image: pgvector/pgvector:pg16
    environment:
      POSTGRES_DB: dnd
      POSTGRES_USER: dnd
      POSTGRES_PASSWORD: dnd
    ports: ["5432:5432"]
    volumes: [pgdata:/var/lib/postgresql/data]

  backend:
    build: ./backend
    ports: ["8000:8000"]
    env_file: .env
    depends_on: [postgres]
    volumes: [./backend:/app]
    command: uvicorn app.main:app --host 0.0.0.0 --port 8000 --reload

  frontend:
    build: ./frontend
    ports: ["5173:5173"]
    volumes: [./frontend:/app]
    command: npm run dev -- --host

volumes:
  pgdata:
```

---

## 11. Environment Configuration

`.env.example`:
```
DATABASE_URL=postgresql+asyncpg://dnd:dnd@postgres:5432/dnd
LLM_BASE_URL=https://openrouter.ai/api/v1
LLM_API_KEY=sk-or-...
LLM_MODEL=deepseek-v4-flash
```

Backend loads via `pydantic-settings` with validation at startup.

---

## 12. Out of Scope (Later Phases)

- Campaign memory / vector search (Phase 1)
- NPC sub-agents (Phase 2)
- Combat state machine (Phase 3)
- Visual board / maps / tokens (Phase 4)
- Voice I/O (Phase 5)
- Multi-system module switching (Phase 6)
- Session journal / summarization
- Message history persistence / replay
- Authentication / accounts
- Multiple characters per player
