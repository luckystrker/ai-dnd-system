# Phase 0: Skeleton & DM Core вЂ” Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** One or more players join a room via code, type actions in chat, and receive streamed narrative responses from an AI Dungeon Master that rolls dice and performs skill checks.

**Architecture:** FastAPI backend with in-process asyncio Event Bus for room pub/sub. LangGraph DM agent streams tokens via WebSocket. React frontend with Zustand state. PostgreSQL for persistence. Docker Compose for dev environment.

**Tech Stack:** Python 3.12, uv, FastAPI, SQLAlchemy 2.0 (async), LangGraph, langchain-openai, PostgreSQL 16 + pgvector, Alembic, React 18, TypeScript, Vite, Zustand, Tailwind CSS, Docker Compose

---

## File Structure

```
ai-dnd-system/
в”њв”Ђв”Ђ docker-compose.yml
в”њв”Ђв”Ђ .env.example
в”њв”Ђв”Ђ .gitignore
в”њв”Ђв”Ђ backend/
в”‚   в”њв”Ђв”Ђ pyproject.toml
в”‚   в”њв”Ђв”Ђ Dockerfile
в”‚   в”њв”Ђв”Ђ alembic.ini
в”‚   в”њв”Ђв”Ђ app/
в”‚   в”‚   в”њв”Ђв”Ђ __init__.py
в”‚   в”‚   в”њв”Ђв”Ђ main.py
в”‚   в”‚   в”њв”Ђв”Ђ config.py
в”‚   в”‚   в”њв”Ђв”Ђ db.py
в”‚   в”‚   в”њв”Ђв”Ђ models/
в”‚   в”‚   в”‚   в”њв”Ђв”Ђ __init__.py
в”‚   в”‚   в”‚   в”њв”Ђв”Ђ room.py
в”‚   в”‚   в”‚   в”њв”Ђв”Ђ player.py
в”‚   в”‚   в”‚   в””в”Ђв”Ђ character.py
в”‚   в”‚   в”њв”Ђв”Ђ schemas/
в”‚   в”‚   в”‚   в”њв”Ђв”Ђ __init__.py
в”‚   в”‚   в”‚   в”њв”Ђв”Ђ room.py
в”‚   в”‚   в”‚   в””в”Ђв”Ђ ws_messages.py
в”‚   в”‚   в”њв”Ђв”Ђ api/
в”‚   в”‚   в”‚   в”њв”Ђв”Ђ __init__.py
в”‚   в”‚   в”‚   в”њв”Ђв”Ђ rooms.py
в”‚   в”‚   в”‚   в””в”Ђв”Ђ ws.py
в”‚   в”‚   в”њв”Ђв”Ђ agents/
в”‚   в”‚   в”‚   в”њв”Ђв”Ђ __init__.py
в”‚   в”‚   в”‚   в”њв”Ђв”Ђ dm_agent.py
в”‚   в”‚   в”‚   в”њв”Ђв”Ђ prompts/
в”‚   в”‚   в”‚   в”‚   в”њв”Ђв”Ђ __init__.py
в”‚   в”‚   в”‚   в”‚   в””в”Ђв”Ђ dm_system.py
в”‚   в”‚   в”‚   в””в”Ђв”Ђ tools/
в”‚   в”‚   в”‚       в”њв”Ђв”Ђ __init__.py
в”‚   в”‚   в”‚       в”њв”Ђв”Ђ dice.py
в”‚   в”‚   в”‚       в””в”Ђв”Ђ checks.py
в”‚   в”‚   в”њв”Ђв”Ђ engine/
в”‚   в”‚   в”‚   в”њв”Ђв”Ђ __init__.py
в”‚   в”‚   в”‚   в”њв”Ђв”Ђ rules_base.py
в”‚   в”‚   в”‚   в””в”Ђв”Ђ systems/
в”‚   в”‚   в”‚       в”њв”Ђв”Ђ __init__.py
в”‚   в”‚   в”‚       в””в”Ђв”Ђ dnd5e.py
в”‚   в”‚   в””в”Ђв”Ђ services/
в”‚   в”‚       в”њв”Ђв”Ђ __init__.py
в”‚   в”‚       в”њв”Ђв”Ђ event_bus.py
в”‚   в”‚       в””в”Ђв”Ђ room_manager.py
в”‚   в”њв”Ђв”Ђ migrations/
в”‚   в”‚   в”њв”Ђв”Ђ env.py
в”‚   в”‚   в”њв”Ђв”Ђ script.py.mako
в”‚   в”‚   в””в”Ђв”Ђ versions/
в”‚   в””в”Ђв”Ђ tests/
в”‚       в”њв”Ђв”Ђ __init__.py
в”‚       в”њв”Ђв”Ђ conftest.py
в”‚       в”њв”Ђв”Ђ test_engine.py
в”‚       в”њв”Ђв”Ђ test_event_bus.py
в”‚       в””в”Ђв”Ђ test_room_manager.py
в”њв”Ђв”Ђ frontend/
в”‚   в”њв”Ђв”Ђ package.json
в”‚   в”њв”Ђв”Ђ Dockerfile
в”‚   в”њв”Ђв”Ђ vite.config.ts
в”‚   в”њв”Ђв”Ђ tailwind.config.ts
в”‚   в”њв”Ђв”Ђ postcss.config.js
в”‚   в”њв”Ђв”Ђ tsconfig.json
в”‚   в”њв”Ђв”Ђ index.html
в”‚   в””в”Ђв”Ђ src/
в”‚       в”њв”Ђв”Ђ main.tsx
в”‚       в”њв”Ђв”Ђ App.tsx
в”‚       в”њв”Ђв”Ђ index.css
в”‚       в”њв”Ђв”Ђ types/
в”‚       в”‚   в””в”Ђв”Ђ messages.ts
в”‚       в”њв”Ђв”Ђ stores/
в”‚       в”‚   в”њв”Ђв”Ђ roomStore.ts
в”‚       в”‚   в””в”Ђв”Ђ chatStore.ts
в”‚       в”њв”Ђв”Ђ hooks/
в”‚       в”‚   в””в”Ђв”Ђ useWebSocket.ts
в”‚       в””в”Ђв”Ђ components/
в”‚           в”њв”Ђв”Ђ Room/
в”‚           в”‚   в”њв”Ђв”Ђ Lobby.tsx
в”‚           в”‚   в””в”Ђв”Ђ RoomView.tsx
в”‚           в””в”Ђв”Ђ Chat/
в”‚               в”њв”Ђв”Ђ ChatWindow.tsx
в”‚               в”њв”Ђв”Ђ MessageList.tsx
в”‚               в”њв”Ђв”Ђ MessageInput.tsx
в”‚               в””в”Ђв”Ђ DiceIndicator.tsx
в””в”Ђв”Ђ docs/
```

---

## Task 1: Project Scaffold & Docker Compose

**Files:**
- Create: `.gitignore`
- Create: `.env.example`
- Create: `docker-compose.yml`
- Create: `backend/Dockerfile`
- Create: `frontend/Dockerfile`

- [ ] **Step 1: Create .gitignore**

```gitignore
# Python
__pycache__/
*.py[cod]
.venv/
*.egg-info/
dist/

# Node
node_modules/
frontend/dist/

# Env
.env

# IDE
.vscode/
.idea/

# OS
.DS_Store
Thumbs.db

# Docker volumes
pgdata/
```

- [ ] **Step 2: Create .env.example**

```env
DATABASE_URL=postgresql+asyncpg://dnd:dnd@postgres:5432/dnd
LLM_BASE_URL=https://openrouter.ai/api/v1
LLM_API_KEY=sk-or-your-key-here
LLM_MODEL=deepseek-v4-flash
```

- [ ] **Step 3: Create docker-compose.yml**

```yaml
services:
  postgres:
    image: pgvector/pgvector:pg16
    environment:
      POSTGRES_DB: dnd
      POSTGRES_USER: dnd
      POSTGRES_PASSWORD: dnd
    ports:
      - "5432:5432"
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U dnd"]
      interval: 5s
      timeout: 3s
      retries: 5

  backend:
    build: ./backend
    ports:
      - "8000:8000"
    env_file: .env
    depends_on:
      postgres:
        condition: service_healthy
    volumes:
      - ./backend:/app
    command: uvicorn app.main:app --host 0.0.0.0 --port 8000 --reload

  frontend:
    build: ./frontend
    ports:
      - "5173:5173"
    volumes:
      - ./frontend:/app
      - /app/node_modules
    command: npm run dev -- --host

volumes:
  pgdata:
```

- [ ] **Step 4: Create backend/Dockerfile**

```dockerfile
FROM python:3.12-slim

WORKDIR /app

COPY --from=ghcr.io/astral-sh/uv:latest /uv /usr/local/bin/uv

COPY pyproject.toml uv.lock* ./
RUN uv sync --frozen --no-dev 2>/dev/null || uv sync --no-dev

COPY . .

EXPOSE 8000
CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8000"]
```

- [ ] **Step 5: Create frontend/Dockerfile**

```dockerfile
FROM node:20-slim

WORKDIR /app

COPY package.json package-lock.json* ./
RUN npm install

COPY . .

EXPOSE 5173
CMD ["npm", "run", "dev", "--", "--host"]
```

- [ ] **Step 6: Commit**

```bash
git add .gitignore .env.example docker-compose.yml backend/Dockerfile frontend/Dockerfile
git commit -m "chore: project scaffold with Docker Compose"
```

---

## Task 2: Backend Init вЂ” Config & Database

**Files:**
- Create: `backend/pyproject.toml`
- Create: `backend/app/__init__.py`
- Create: `backend/app/config.py`
- Create: `backend/app/db.py`
- Create: `backend/app/main.py`

- [ ] **Step 1: Initialize backend with uv**

Run:
```bash
cd backend
uv init --name ttrpg-backend --python 3.12
```

Then replace `pyproject.toml` content:

```toml
[project]
name = "ttrpg-backend"
version = "0.1.0"
description = "TTRPG Agent System Backend"
requires-python = ">=3.12"
dependencies = [
    "fastapi>=0.115.0",
    "uvicorn[standard]>=0.30.0",
    "sqlalchemy[asyncio]>=2.0.0",
    "asyncpg>=0.29.0",
    "alembic>=1.13.0",
    "pydantic>=2.0.0",
    "pydantic-settings>=2.0.0",
    "langchain>=0.3.0",
    "langchain-openai>=0.2.0",
    "langgraph>=0.2.0",
    "websockets>=12.0",
]

[project.optional-dependencies]
dev = [
    "pytest>=8.0.0",
    "pytest-asyncio>=0.24.0",
    "httpx>=0.27.0",
]

[build-system]
requires = ["hatchling"]
build-backend = "hatchling.build"
```

- [ ] **Step 2: Install dependencies**

Run:
```bash
cd backend
uv sync --all-extras
```

- [ ] **Step 3: Create app/config.py**

```python
from pydantic_settings import BaseSettings


class Settings(BaseSettings):
    database_url: str = "postgresql+asyncpg://dnd:dnd@localhost:5432/dnd"
    llm_base_url: str = "https://openrouter.ai/api/v1"
    llm_api_key: str = ""
    llm_model: str = "deepseek-v4-flash"

    model_config = {"env_file": ".env", "env_file_encoding": "utf-8"}


settings = Settings()
```

- [ ] **Step 4: Create app/db.py**

```python
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker, create_async_engine

from app.config import settings

engine = create_async_engine(settings.database_url, echo=False)
async_session_factory = async_sessionmaker(engine, class_=AsyncSession, expire_on_commit=False)


async def get_session() -> AsyncSession:
    async with async_session_factory() as session:
        yield session
```

- [ ] **Step 5: Create app/main.py**

```python
from contextlib import asynccontextmanager

from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware


@asynccontextmanager
async def lifespan(app: FastAPI):
    yield


app = FastAPI(title="TTRPG Agent System", version="0.1.0", lifespan=lifespan)

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)


@app.get("/health")
async def health():
    return {"status": "ok"}
```

- [ ] **Step 6: Create app/__init__.py (empty)**

```python
```

- [ ] **Step 7: Verify backend starts**

Run:
```bash
cd backend
uv run uvicorn app.main:app --port 8000
```

Expected: Server starts, `GET http://localhost:8000/health` returns `{"status": "ok"}`

- [ ] **Step 8: Commit**

```bash
git add backend/
git commit -m "feat(backend): FastAPI skeleton with config and database setup"
```

---

## Task 3: Database Models & Migrations

**Files:**
- Create: `backend/app/models/__init__.py`
- Create: `backend/app/models/room.py`
- Create: `backend/app/models/player.py`
- Create: `backend/app/models/character.py`
- Create: `backend/alembic.ini`
- Create: `backend/migrations/env.py`
- Create: `backend/migrations/script.py.mako`

- [ ] **Step 1: Create app/models/room.py**

```python
import uuid
from datetime import datetime

from sqlalchemy import DateTime, String, func
from sqlalchemy.dialects.postgresql import JSONB, UUID
from sqlalchemy.orm import DeclarativeBase, Mapped, mapped_column


class Base(DeclarativeBase):
    pass


class Room(Base):
    __tablename__ = "rooms"

    id: Mapped[uuid.UUID] = mapped_column(UUID(as_uuid=True), primary_key=True, default=uuid.uuid4)
    code: Mapped[str] = mapped_column(String(6), unique=True, nullable=False)
    config: Mapped[dict] = mapped_column(JSONB, default=dict)
    status: Mapped[str] = mapped_column(String(20), default="active")
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), server_default=func.now())
```

- [ ] **Step 2: Create app/models/player.py**

```python
import uuid
from datetime import datetime

from sqlalchemy import DateTime, String, func
from sqlalchemy.dialects.postgresql import UUID
from sqlalchemy.orm import Mapped, mapped_column

from app.models.room import Base


class Player(Base):
    __tablename__ = "players"

    id: Mapped[uuid.UUID] = mapped_column(UUID(as_uuid=True), primary_key=True, default=uuid.uuid4)
    token: Mapped[str] = mapped_column(String(64), unique=True, nullable=False)
    display_name: Mapped[str] = mapped_column(String(50), nullable=False)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), server_default=func.now())
```

- [ ] **Step 3: Create app/models/character.py**

```python
import uuid
from datetime import datetime

from sqlalchemy import DateTime, ForeignKey, String, func
from sqlalchemy.dialects.postgresql import JSONB, UUID
from sqlalchemy.orm import Mapped, mapped_column

from app.models.room import Base


class RoomPlayer(Base):
    __tablename__ = "room_players"

    room_id: Mapped[uuid.UUID] = mapped_column(UUID(as_uuid=True), ForeignKey("rooms.id"), primary_key=True)
    player_id: Mapped[uuid.UUID] = mapped_column(UUID(as_uuid=True), ForeignKey("players.id"), primary_key=True)
    joined_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), server_default=func.now())


class Character(Base):
    __tablename__ = "characters"

    id: Mapped[uuid.UUID] = mapped_column(UUID(as_uuid=True), primary_key=True, default=uuid.uuid4)
    room_id: Mapped[uuid.UUID] = mapped_column(UUID(as_uuid=True), ForeignKey("rooms.id"), nullable=False)
    player_id: Mapped[uuid.UUID] = mapped_column(UUID(as_uuid=True), ForeignKey("players.id"), nullable=False)
    name: Mapped[str] = mapped_column(String(100), nullable=False)
    stats: Mapped[dict] = mapped_column(JSONB, default=dict)
    inventory: Mapped[list] = mapped_column(JSONB, default=list)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), server_default=func.now())
```

- [ ] **Step 4: Create app/models/__init__.py**

```python
from app.models.room import Base, Room
from app.models.player import Player
from app.models.character import Character, RoomPlayer

__all__ = ["Base", "Room", "Player", "Character", "RoomPlayer"]
```

- [ ] **Step 5: Initialize Alembic**

Run:
```bash
cd backend
uv run alembic init migrations
```

- [ ] **Step 6: Edit migrations/env.py вЂ” set target_metadata and database_url**

Replace the relevant parts of `migrations/env.py`:

```python
from app.config import settings
from app.models import Base

target_metadata = Base.metadata

# In run_migrations_online(), replace sqlalchemy.url with:
# config.set_main_option("sqlalchemy.url", settings.database_url)
```

Specifically, add at the top of `run_migrations_online()`:
```python
configuration = config.get_section(config.config_ini_section, {})
configuration["sqlalchemy.url"] = settings.database_url
connectable = engine_from_config(configuration, prefix="sqlalchemy.", poolclass=pool.NullPool)
```

- [ ] **Step 7: Edit alembic.ini вЂ” remove hardcoded sqlalchemy.url**

Set `sqlalchemy.url =` (empty) in `alembic.ini` since we provide it programmatically.

- [ ] **Step 8: Generate initial migration**

Run:
```bash
cd backend
uv run alembic revision --autogenerate -m "initial schema"
```

Expected: Creates `migrations/versions/xxxx_initial_schema.py` with rooms, players, room_players, characters tables.

- [ ] **Step 9: Commit**

```bash
git add backend/app/models/ backend/alembic.ini backend/migrations/
git commit -m "feat(backend): database models and initial migration"
```

---

## Task 4: Pydantic Schemas

**Files:**
- Create: `backend/app/schemas/__init__.py`
- Create: `backend/app/schemas/room.py`
- Create: `backend/app/schemas/ws_messages.py`

- [ ] **Step 1: Create app/schemas/room.py**

```python
from pydantic import BaseModel, Field


class CreateRoomRequest(BaseModel):
    display_name: str = Field(min_length=1, max_length=50)


class JoinRoomRequest(BaseModel):
    code: str = Field(min_length=6, max_length=6)
    display_name: str = Field(min_length=1, max_length=50)
    player_token: str | None = None


class CreateRoomResponse(BaseModel):
    room_id: str
    code: str
    player_token: str


class JoinRoomResponse(BaseModel):
    room_id: str
    player_token: str


class PlayerInfo(BaseModel):
    id: str
    display_name: str


class RoomStateResponse(BaseModel):
    room_id: str
    code: str
    status: str
    players: list[PlayerInfo]
```

- [ ] **Step 2: Create app/schemas/ws_messages.py**

```python
from datetime import datetime
from typing import Any

from pydantic import BaseModel


class WSMessage(BaseModel):
    type: str
    payload: dict[str, Any] = {}


class Event(BaseModel):
    type: str
    payload: dict[str, Any] = {}
    timestamp: datetime = datetime.now()
    sender_id: str | None = None
```

- [ ] **Step 3: Create app/schemas/__init__.py**

```python
from app.schemas.room import (
    CreateRoomRequest,
    CreateRoomResponse,
    JoinRoomRequest,
    JoinRoomResponse,
    RoomStateResponse,
)
from app.schemas.ws_messages import Event, WSMessage

__all__ = [
    "CreateRoomRequest",
    "CreateRoomResponse",
    "JoinRoomRequest",
    "JoinRoomResponse",
    "RoomStateResponse",
    "Event",
    "WSMessage",
]
```

- [ ] **Step 4: Commit**

```bash
git add backend/app/schemas/
git commit -m "feat(backend): pydantic schemas for REST and WebSocket"
```

---

## Task 5: Room Manager Service

**Files:**
- Create: `backend/app/services/__init__.py`
- Create: `backend/app/services/room_manager.py`
- Create: `backend/tests/__init__.py`
- Create: `backend/tests/conftest.py`
- Create: `backend/tests/test_room_manager.py`

- [ ] **Step 1: Write the failing test**

Create `backend/tests/conftest.py`:

```python
import pytest


@pytest.fixture
def anyio_backend():
    return "asyncio"
```

Create `backend/tests/test_room_manager.py`:

```python
from app.services.room_manager import generate_room_code, generate_token


def test_generate_room_code_length():
    code = generate_room_code()
    assert len(code) == 6


def test_generate_room_code_no_ambiguous_chars():
    ambiguous = set("0O1I")
    for _ in range(100):
        code = generate_room_code()
        assert not set(code) & ambiguous


def test_generate_token_length():
    token = generate_token()
    assert len(token) == 64


def test_generate_token_is_hex():
    token = generate_token()
    int(token, 16)
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && uv run pytest tests/test_room_manager.py -v`
Expected: FAIL вЂ” `ModuleNotFoundError`

- [ ] **Step 3: Implement room_manager.py**

Create `backend/app/services/__init__.py` (empty).
Create `backend/app/services/room_manager.py`:

```python
import secrets
import string

from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

from app.models.character import RoomPlayer
from app.models.player import Player
from app.models.room import Room

CODE_ALPHABET = "".join(c for c in string.ascii_uppercase + string.digits if c not in "0O1I")


def generate_room_code(length: int = 6) -> str:
    return "".join(secrets.choice(CODE_ALPHABET) for _ in range(length))


def generate_token() -> str:
    return secrets.token_hex(32)


async def create_room(session: AsyncSession, display_name: str) -> tuple[Room, Player]:
    room = Room(code=generate_room_code())
    session.add(room)
    player = Player(token=generate_token(), display_name=display_name)
    session.add(player)
    await session.flush()
    session.add(RoomPlayer(room_id=room.id, player_id=player.id))
    await session.commit()
    await session.refresh(room)
    await session.refresh(player)
    return room, player


async def join_room(
    session: AsyncSession, code: str, display_name: str, player_token: str | None = None
) -> tuple[Room, Player]:
    result = await session.execute(select(Room).where(Room.code == code))
    room = result.scalar_one_or_none()
    if room is None:
        raise ValueError(f"Room with code {code} not found")

    player: Player | None = None
    if player_token:
        result = await session.execute(select(Player).where(Player.token == player_token))
        player = result.scalar_one_or_none()

    if player is None:
        player = Player(token=generate_token(), display_name=display_name)
        session.add(player)
        await session.flush()

    existing = await session.execute(
        select(RoomPlayer).where(RoomPlayer.room_id == room.id, RoomPlayer.player_id == player.id)
    )
    if existing.scalar_one_or_none() is None:
        session.add(RoomPlayer(room_id=room.id, player_id=player.id))

    await session.commit()
    await session.refresh(room)
    await session.refresh(player)
    return room, player


async def get_room_state(session: AsyncSession, code: str) -> dict | None:
    result = await session.execute(select(Room).where(Room.code == code))
    room = result.scalar_one_or_none()
    if room is None:
        return None
    players_result = await session.execute(
        select(Player)
        .join(RoomPlayer, RoomPlayer.player_id == Player.id)
        .where(RoomPlayer.room_id == room.id)
    )
    players = players_result.scalars().all()
    return {
        "room_id": str(room.id),
        "code": room.code,
        "status": room.status,
        "players": [{"id": str(p.id), "display_name": p.display_name} for p in players],
    }
```

- [ ] **Step 4: Run tests** вЂ” Expected: 4 PASS

- [ ] **Step 5: Commit**

```bash
git add backend/app/services/ backend/tests/
git commit -m "feat(backend): room manager service with code/token generation"
```

---

## Task 6: Event Bus Service

**Files:**
- Create: `backend/app/services/event_bus.py`
- Create: `backend/tests/test_event_bus.py`

- [ ] **Step 1: Write the failing test**

Create `backend/tests/test_event_bus.py`:

```python
from unittest.mock import AsyncMock
import pytest
from app.schemas.ws_messages import Event
from app.services.event_bus import EventBus


@pytest.fixture
def bus():
    return EventBus()


@pytest.mark.asyncio
async def test_publish_to_subscriber(bus):
    ws = AsyncMock()
    bus.subscribe("room1", ws)
    await bus.publish("room1", Event(type="test", payload={"msg": "hello"}))
    ws.send_text.assert_called_once()
    assert '"test"' in ws.send_text.call_args[0][0]


@pytest.mark.asyncio
async def test_publish_to_multiple(bus):
    ws1, ws2 = AsyncMock(), AsyncMock()
    bus.subscribe("room1", ws1)
    bus.subscribe("room1", ws2)
    await bus.publish("room1", Event(type="test", payload={}))
    ws1.send_text.assert_called_once()
    ws2.send_text.assert_called_once()


@pytest.mark.asyncio
async def test_unsubscribe(bus):
    ws = AsyncMock()
    bus.subscribe("room1", ws)
    bus.unsubscribe("room1", ws)
    await bus.publish("room1", Event(type="test", payload={}))
    ws.send_text.assert_not_called()


@pytest.mark.asyncio
async def test_publish_empty_room(bus):
    await bus.publish("nope", Event(type="test", payload={}))
```

- [ ] **Step 2: Run test** вЂ” Expected: FAIL

- [ ] **Step 3: Implement event_bus.py**

```python
from collections import defaultdict
from fastapi import WebSocket
from app.schemas.ws_messages import Event


class EventBus:
    def __init__(self):
        self._subscribers: dict[str, set[WebSocket]] = defaultdict(set)

    def subscribe(self, room_id: str, ws: WebSocket) -> None:
        self._subscribers[room_id].add(ws)

    def unsubscribe(self, room_id: str, ws: WebSocket) -> None:
        self._subscribers[room_id].discard(ws)
        if not self._subscribers[room_id]:
            del self._subscribers[room_id]

    async def publish(self, room_id: str, event: Event) -> None:
        message = event.model_dump_json()
        dead: list[WebSocket] = []
        for ws in self._subscribers.get(room_id, set()):
            try:
                await ws.send_text(message)
            except Exception:
                dead.append(ws)
        for ws in dead:
            self.unsubscribe(room_id, ws)

    def subscriber_count(self, room_id: str) -> int:
        return len(self._subscribers.get(room_id, set()))


event_bus = EventBus()
```

- [ ] **Step 4: Run tests** вЂ” Expected: 4 PASS

- [ ] **Step 5: Commit**

```bash
git add backend/app/services/event_bus.py backend/tests/test_event_bus.py
git commit -m "feat(backend): in-process asyncio event bus for room pub/sub"
```

---

## Task 7: Rules Engine

**Files:**
- Create: `backend/app/engine/__init__.py`
- Create: `backend/app/engine/rules_base.py`
- Create: `backend/app/engine/systems/__init__.py`
- Create: `backend/app/engine/systems/dnd5e.py`
- Create: `backend/tests/test_engine.py`

- [ ] **Step 1: Write failing tests**

Create `backend/tests/test_engine.py`:

```python
import pytest
from app.engine.systems.dnd5e import DnD5eSystem


@pytest.fixture
def system():
    return DnD5eSystem()


def test_roll_dice_single(system):
    result = system.roll_dice(sides=20, count=1)
    assert len(result.rolls) == 1
    assert 1 <= result.rolls[0] <= 20
    assert result.total == result.rolls[0]


def test_roll_dice_multiple(system):
    result = system.roll_dice(sides=6, count=3)
    assert len(result.rolls) == 3
    assert all(1 <= r <= 6 for r in result.rolls)
    assert result.total == sum(result.rolls)


def test_skill_check_modifier(system):
    result = system.skill_check({"str": 20}, "athletics", difficulty=10)
    assert result.modifier == 5
    assert result.total == result.roll + 5


def test_skill_check_correct_ability(system):
    result = system.skill_check({"dex": 18, "str": 8}, "stealth", difficulty=10)
    assert result.modifier == 4


def test_skill_check_missing_stat(system):
    result = system.skill_check({}, "athletics", difficulty=10)
    assert result.modifier == 0


def test_advantage(system):
    results = [system.skill_check({"str": 10}, "athletics", 10, advantage=True) for _ in range(50)]
    assert sum(r.roll for r in results) / 50 > 11


def test_disadvantage(system):
    results = [system.skill_check({"str": 10}, "athletics", 10, advantage=False) for _ in range(50)]
    assert sum(r.roll for r in results) / 50 < 10
```

- [ ] **Step 2: Run tests** вЂ” Expected: FAIL

- [ ] **Step 3: Implement rules_base.py**

```python
from abc import ABC, abstractmethod
from pydantic import BaseModel


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


class GameSystem(ABC):
    @abstractmethod
    def roll_dice(self, sides: int, count: int = 1) -> DiceResult: ...

    @abstractmethod
    def skill_check(self, stats: dict, skill: str, difficulty: int, advantage: bool | None = None) -> CheckResult: ...
```

- [ ] **Step 4: Implement dnd5e.py**

```python
import random
from app.engine.rules_base import CheckResult, DiceResult, GameSystem

SKILL_ABILITY_MAP: dict[str, str] = {
    "athletics": "str", "acrobatics": "dex", "sleight_of_hand": "dex", "stealth": "dex",
    "arcana": "int", "history": "int", "investigation": "int", "nature": "int", "religion": "int",
    "animal_handling": "wis", "insight": "wis", "medicine": "wis", "perception": "wis", "survival": "wis",
    "deception": "cha", "intimidation": "cha", "performance": "cha", "persuasion": "cha",
}


class DnD5eSystem(GameSystem):
    def roll_dice(self, sides: int, count: int = 1) -> DiceResult:
        rolls = [random.randint(1, sides) for _ in range(count)]
        return DiceResult(rolls=rolls, total=sum(rolls))

    def skill_check(self, stats: dict, skill: str, difficulty: int, advantage: bool | None = None) -> CheckResult:
        ability = SKILL_ABILITY_MAP.get(skill.lower(), "str")
        score = stats.get(ability, 10)
        modifier = (score - 10) // 2
        if advantage is True:
            roll = max(random.randint(1, 20), random.randint(1, 20))
        elif advantage is False:
            roll = min(random.randint(1, 20), random.randint(1, 20))
        else:
            roll = random.randint(1, 20)
        total = roll + modifier
        return CheckResult(roll=roll, modifier=modifier, total=total, difficulty=difficulty, success=total >= difficulty, margin=total - difficulty)
```

- [ ] **Step 5: Run tests** вЂ” Expected: 7 PASS

- [ ] **Step 6: Commit**

```bash
git add backend/app/engine/ backend/tests/test_engine.py
git commit -m "feat(backend): D&D 5e rules engine with dice and skill checks"
```

---

## Task 8: DM Agent Tools

**Files:**
- Create: `backend/app/agents/__init__.py`
- Create: `backend/app/agents/tools/__init__.py`
- Create: `backend/app/agents/tools/dice.py`
- Create: `backend/app/agents/tools/checks.py`

- [ ] **Step 1: Create empty __init__.py files**

- [ ] **Step 2: Create tools/dice.py**

```python
from langchain_core.tools import tool
from app.engine.systems.dnd5e import DnD5eSystem

_system = DnD5eSystem()


@tool
def roll_dice(sides: int, count: int = 1) -> str:
    """Roll dice. Specify sides and count. Example: roll_dice(sides=20) for d20."""
    result = _system.roll_dice(sides=sides, count=count)
    return f"Rolled {count}d{sides}: {result.rolls} = {result.total}"
```

- [ ] **Step 3: Create tools/checks.py**

```python
from langchain_core.tools import tool
from app.engine.systems.dnd5e import DnD5eSystem

_system = DnD5eSystem()
_current_stats: dict = {}


def set_character_stats(stats: dict) -> None:
    global _current_stats
    _current_stats = stats


@tool
def skill_check(character_name: str, skill: str, difficulty: int) -> str:
    """Perform a skill check. Rolls d20 + modifier vs difficulty. Skills: athletics, stealth, perception, persuasion, arcana, etc."""
    result = _system.skill_check(stats=_current_stats, skill=skill, difficulty=difficulty)
    status = "SUCCESS" if result.success else "FAILURE"
    return f"{character_name} вЂ” {skill}: {result.roll} + {result.modifier} = {result.total} vs DC {difficulty} в†’ {status}"
```

- [ ] **Step 4: Commit**

```bash
git add backend/app/agents/
git commit -m "feat(backend): LangChain tools for dice and skill checks"
```

---

## Task 9: DM Agent (LangGraph)

**Files:**
- Create: `backend/app/agents/prompts/__init__.py`
- Create: `backend/app/agents/prompts/dm_system.py`
- Create: `backend/app/agents/dm_agent.py`

- [ ] **Step 1: Create prompts/dm_system.py**

```python
DM_SYSTEM_PROMPT = """You are a Dungeon Master for a tabletop role-playing game. You narrate the world, control NPCs, and respond to player actions.

## Your Role
- Describe scenes, environments, and NPC reactions vividly but concisely (2-4 paragraphs)
- React to player actions logically вЂ” the world responds believably
- When an action has uncertain outcome, USE YOUR TOOLS to roll dice or perform skill checks. NEVER invent dice results.
- After a tool result, narrate the outcome dramatically

## Tone
- High fantasy adventure: dungeons, dragons, ancient ruins, heroic deeds
- Casual and fun вЂ” narrative over strict rules
- Address the player directly: "You see...", "You feel..."

## Tools
- `roll_dice(sides, count)`: Roll any dice combination
- `skill_check(character_name, skill, difficulty)`: Ability check with d20 + modifier

## Rules
- Default difficulty: 10 (easy), 15 (medium), 20 (hard)
- Only call tools when outcome is uncertain. Walking through a door doesn't need a roll.
- After receiving a tool result, ALWAYS narrate what happened based on success/failure
"""
```

- [ ] **Step 2: Create dm_agent.py**

```python
import asyncio
from typing import AsyncGenerator

from langchain_core.messages import AIMessage, HumanMessage, SystemMessage
from langchain_openai import ChatOpenAI
from langgraph.graph import END, MessagesState, StateGraph
from langgraph.prebuilt import ToolNode

from app.agents.prompts.dm_system import DM_SYSTEM_PROMPT
from app.agents.tools.checks import set_character_stats, skill_check
from app.agents.tools.dice import roll_dice
from app.config import settings
from app.schemas.ws_messages import Event
from app.services.event_bus import event_bus

tools = [roll_dice, skill_check]

llm = ChatOpenAI(
    base_url=settings.llm_base_url,
    api_key=settings.llm_api_key,
    model=settings.llm_model,
    streaming=True,
).bind_tools(tools)


def _build_graph():
    def agent_node(state: MessagesState):
        messages = [SystemMessage(content=DM_SYSTEM_PROMPT)] + state["messages"]
        response = llm.invoke(messages)
        return {"messages": [response]}

    def should_continue(state: MessagesState):
        last = state["messages"][-1]
        if hasattr(last, "tool_calls") and last.tool_calls:
            return "tools"
        return END

    graph = StateGraph(MessagesState)
    graph.add_node("agent", agent_node)
    graph.add_node("tools", ToolNode(tools))
    graph.set_entry_point("agent")
    graph.add_conditional_edges("agent", should_continue, {"tools": "tools", END: END})
    graph.add_edge("tools", "agent")
    return graph.compile()


dm_graph = _build_graph()


async def run_dm_agent(
    room_id: str, player_message: str, character_stats: dict | None = None
) -> None:
    """Run the DM agent and stream results to the room via event bus."""
    if character_stats:
        set_character_stats(character_stats)

    input_messages = {"messages": [HumanMessage(content=player_message)]}
    full_text = ""

    try:
        async for event in dm_graph.astream_events(input_messages, version="v2"):
            kind = event["event"]

            if kind == "on_chat_model_stream":
                chunk = event["data"]["chunk"]
                if hasattr(chunk, "content") and chunk.content:
                    full_text += chunk.content
                    await event_bus.publish(
                        room_id, Event(type="dm_token", payload={"token": chunk.content})
                    )

            elif kind == "on_tool_end":
                tool_output = event["data"].get("output", "")
                tool_name = event.get("name", "unknown")
                await event_bus.publish(
                    room_id,
                    Event(type="dice_roll", payload={"tool": tool_name, "result": str(tool_output)}),
                )

        await event_bus.publish(
            room_id,
            Event(type="dm_complete", payload={"full_text": full_text}),
        )
    except Exception as e:
        await event_bus.publish(
            room_id, Event(type="error", payload={"message": f"DM error: {str(e)}"})
        )
```

- [ ] **Step 3: Commit**

```bash
git add backend/app/agents/
git commit -m "feat(backend): LangGraph DM agent with streaming and tool integration"
```

---

## Task 10: REST API & WebSocket Handler

**Files:**
- Create: `backend/app/api/__init__.py`
- Create: `backend/app/api/rooms.py`
- Create: `backend/app/api/ws.py`
- Modify: `backend/app/main.py`

- [ ] **Step 1: Create api/rooms.py**

```python
from fastapi import APIRouter, Depends, HTTPException
from sqlalchemy.ext.asyncio import AsyncSession

from app.db import get_session
from app.schemas.room import (
    CreateRoomRequest,
    CreateRoomResponse,
    JoinRoomRequest,
    JoinRoomResponse,
    RoomStateResponse,
)
from app.services import room_manager

router = APIRouter(prefix="/room", tags=["rooms"])


@router.post("/create", response_model=CreateRoomResponse)
async def create_room(req: CreateRoomRequest, session: AsyncSession = Depends(get_session)):
    room, player = await room_manager.create_room(session, req.display_name)
    return CreateRoomResponse(room_id=str(room.id), code=room.code, player_token=player.token)


@router.post("/join", response_model=JoinRoomResponse)
async def join_room(req: JoinRoomRequest, session: AsyncSession = Depends(get_session)):
    try:
        room, player = await room_manager.join_room(
            session, req.code, req.display_name, req.player_token
        )
    except ValueError as e:
        raise HTTPException(status_code=404, detail=str(e))
    return JoinRoomResponse(room_id=str(room.id), player_token=player.token)


@router.get("/{code}", response_model=RoomStateResponse)
async def get_room(code: str, session: AsyncSession = Depends(get_session)):
    state = await room_manager.get_room_state(session, code)
    if state is None:
        raise HTTPException(status_code=404, detail="Room not found")
    return state
```

- [ ] **Step 2: Create api/ws.py**

```python
import asyncio
import json

from fastapi import APIRouter, WebSocket, WebSocketDisconnect

from app.agents.dm_agent import run_dm_agent
from app.schemas.ws_messages import Event
from app.services.event_bus import event_bus

router = APIRouter()

# Per-room message queue for sequential DM processing
_room_queues: dict[str, asyncio.Queue] = {}


async def _process_queue(room_id: str, queue: asyncio.Queue):
    """Process player messages sequentially per room."""
    while True:
        message_text = await queue.get()
        await run_dm_agent(room_id, message_text)
        queue.task_done()


@router.websocket("/ws/room/{code}")
async def websocket_room(websocket: WebSocket, code: str, token: str = ""):
    await websocket.accept()

    room_id = code  # Use code as room identifier for event bus
    event_bus.subscribe(room_id, websocket)

    # Ensure room has a processing queue
    if room_id not in _room_queues:
        _room_queues[room_id] = asyncio.Queue()
        asyncio.create_task(_process_queue(room_id, _room_queues[room_id]))

    # Notify others
    await event_bus.publish(
        room_id, Event(type="player_joined", payload={"name": f"Player ({token[:8]}...)"})
    )

    try:
        while True:
            raw = await websocket.receive_text()
            data = json.loads(raw)
            msg_type = data.get("type")

            if msg_type == "ping":
                await websocket.send_text(json.dumps({"type": "pong", "payload": {}}))

            elif msg_type == "player_message":
                text = data.get("payload", {}).get("text", "").strip()
                if not text:
                    continue
                # Broadcast player message to all
                await event_bus.publish(
                    room_id,
                    Event(type="player_message", payload={"sender": "player", "text": text}),
                )
                # Queue for DM processing
                await _room_queues[room_id].put(text)

    except WebSocketDisconnect:
        pass
    finally:
        event_bus.unsubscribe(room_id, websocket)
```

- [ ] **Step 3: Update main.py to include routers**

Add to `backend/app/main.py` after the CORS middleware:

```python
from app.api import rooms, ws

app.include_router(rooms.router)
app.include_router(ws.router)
```

- [ ] **Step 4: Create api/__init__.py (empty)**

- [ ] **Step 5: Verify server starts**

Run: `cd backend && uv run uvicorn app.main:app --port 8000`
Expected: Server starts without errors. `GET /health` returns 200.

- [ ] **Step 6: Commit**

```bash
git add backend/app/api/ backend/app/main.py
git commit -m "feat(backend): REST API for rooms and WebSocket handler with DM streaming"
```

---

## Task 11: Frontend Scaffold

**Files:**
- Create: `frontend/package.json`
- Create: `frontend/vite.config.ts`
- Create: `frontend/tailwind.config.ts`
- Create: `frontend/postcss.config.js`
- Create: `frontend/tsconfig.json`
- Create: `frontend/index.html`
- Create: `frontend/src/main.tsx`
- Create: `frontend/src/index.css`
- Create: `frontend/src/App.tsx`

- [ ] **Step 1: Initialize frontend with Vite**

Run:
```bash
cd frontend
npm create vite@latest . -- --template react-ts
npm install
npm install zustand
npm install -D tailwindcss @tailwindcss/vite
```

- [ ] **Step 2: Configure vite.config.ts**

```typescript
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    host: true,
    port: 5173,
    proxy: {
      "/room": "http://localhost:8000",
      "/ws": { target: "ws://localhost:8000", ws: true },
      "/health": "http://localhost:8000",
    },
  },
});
```

- [ ] **Step 3: Replace src/index.css**

```css
@import "tailwindcss";
```

- [ ] **Step 4: Create src/App.tsx (minimal router)**

```tsx
import { useState } from "react";
import Lobby from "./components/Room/Lobby";
import RoomView from "./components/Room/RoomView";

export default function App() {
  const [roomCode, setRoomCode] = useState<string | null>(null);

  if (!roomCode) {
    return <Lobby onJoin={(code) => setRoomCode(code)} />;
  }
  return <RoomView code={roomCode} onLeave={() => setRoomCode(null)} />;
}
```

- [ ] **Step 5: Verify frontend starts**

Run: `cd frontend && npm run dev`
Expected: Vite dev server starts on port 5173

- [ ] **Step 6: Commit**

```bash
git add frontend/
git commit -m "feat(frontend): Vite + React + Tailwind scaffold"
```

---

## Task 12: Frontend вЂ” Types, Stores, WebSocket Hook

**Files:**
- Create: `frontend/src/types/messages.ts`
- Create: `frontend/src/stores/roomStore.ts`
- Create: `frontend/src/stores/chatStore.ts`
- Create: `frontend/src/hooks/useWebSocket.ts`

- [ ] **Step 1: Create types/messages.ts**

```typescript
export interface WSMessage {
  type: string;
  payload: Record<string, unknown>;
}

export interface ChatMessage {
  id: string;
  sender: "player" | "dm" | "system";
  senderName?: string;
  text: string;
  type: "message" | "dice_roll";
  timestamp: number;
}
```

- [ ] **Step 2: Create stores/roomStore.ts**

```typescript
import { create } from "zustand";

interface RoomState {
  roomCode: string | null;
  playerToken: string | null;
  playerName: string;
  players: string[];
  connected: boolean;
  setRoom: (code: string, token: string, name: string) => void;
  setConnected: (v: boolean) => void;
  setPlayers: (players: string[]) => void;
  reset: () => void;
}

export const useRoomStore = create<RoomState>((set) => ({
  roomCode: null,
  playerToken: null,
  playerName: "",
  players: [],
  connected: false,
  setRoom: (code, token, name) => set({ roomCode: code, playerToken: token, playerName: name }),
  setConnected: (v) => set({ connected: v }),
  setPlayers: (players) => set({ players }),
  reset: () => set({ roomCode: null, playerToken: null, playerName: "", players: [], connected: false }),
}));
```

- [ ] **Step 3: Create stores/chatStore.ts**

```typescript
import { create } from "zustand";
import { ChatMessage } from "../types/messages";

interface ChatState {
  messages: ChatMessage[];
  streamingText: string;
  isStreaming: boolean;
  addMessage: (msg: ChatMessage) => void;
  appendToken: (token: string) => void;
  finalizeStream: () => void;
  clear: () => void;
}

export const useChatStore = create<ChatState>((set) => ({
  messages: [],
  streamingText: "",
  isStreaming: false,
  addMessage: (msg) => set((s) => ({ messages: [...s.messages, msg] })),
  appendToken: (token) =>
    set((s) => ({ streamingText: s.streamingText + token, isStreaming: true })),
  finalizeStream: () =>
    set((s) => {
      if (!s.streamingText) return { streamingText: "", isStreaming: false };
      const msg: ChatMessage = {
        id: crypto.randomUUID(),
        sender: "dm",
        senderName: "Dungeon Master",
        text: s.streamingText,
        type: "message",
        timestamp: Date.now(),
      };
      return { messages: [...s.messages, msg], streamingText: "", isStreaming: false };
    }),
  clear: () => set({ messages: [], streamingText: "", isStreaming: false }),
}));
```

- [ ] **Step 4: Create hooks/useWebSocket.ts**

```typescript
import { useEffect, useRef } from "react";
import { useChatStore } from "../stores/chatStore";
import { useRoomStore } from "../stores/roomStore";
import { ChatMessage } from "../types/messages";

export function useWebSocket(code: string | null) {
  const wsRef = useRef<WebSocket | null>(null);
  const { addMessage, appendToken, finalizeStream } = useChatStore();
  const { playerToken, setConnected } = useRoomStore();

  useEffect(() => {
    if (!code) return;

    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const url = `${protocol}//${window.location.host}/ws/room/${code}?token=${playerToken || ""}`;
    const ws = new WebSocket(url);
    wsRef.current = ws;

    ws.onopen = () => setConnected(true);
    ws.onclose = () => setConnected(false);

    ws.onmessage = (event) => {
      const data = JSON.parse(event.data);
      switch (data.type) {
        case "dm_token":
          appendToken(data.payload.token);
          break;
        case "dm_complete":
          finalizeStream();
          break;
        case "player_message": {
          const msg: ChatMessage = {
            id: crypto.randomUUID(),
            sender: "player",
            senderName: data.payload.sender,
            text: data.payload.text,
            type: "message",
            timestamp: Date.now(),
          };
          addMessage(msg);
          break;
        }
        case "dice_roll": {
          const msg: ChatMessage = {
            id: crypto.randomUUID(),
            sender: "system",
            text: `рџЋІ ${data.payload.result}`,
            type: "dice_roll",
            timestamp: Date.now(),
          };
          addMessage(msg);
          break;
        }
        case "player_joined": {
          const msg: ChatMessage = {
            id: crypto.randomUUID(),
            sender: "system",
            text: `${data.payload.name} joined the room`,
            type: "message",
            timestamp: Date.now(),
          };
          addMessage(msg);
          break;
        }
      }
    };

    const pingInterval = setInterval(() => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: "ping", payload: {} }));
      }
    }, 30000);

    return () => {
      clearInterval(pingInterval);
      ws.close();
    };
  }, [code, playerToken]);

  const sendMessage = (text: string) => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify({ type: "player_message", payload: { text } }));
    }
  };

  return { sendMessage };
}
```

- [ ] **Step 5: Commit**

```bash
git add frontend/src/types/ frontend/src/stores/ frontend/src/hooks/
git commit -m "feat(frontend): Zustand stores, types, and WebSocket hook"
```

---

## Task 13: Frontend вЂ” UI Components

**Files:**
- Create: `frontend/src/components/Room/Lobby.tsx`
- Create: `frontend/src/components/Room/RoomView.tsx`
- Create: `frontend/src/components/Chat/ChatWindow.tsx`
- Create: `frontend/src/components/Chat/MessageList.tsx`
- Create: `frontend/src/components/Chat/MessageInput.tsx`
- Create: `frontend/src/components/Chat/DiceIndicator.tsx`

- [ ] **Step 1: Create Lobby.tsx**

```tsx
import { useState } from "react";
import { useRoomStore } from "../../stores/roomStore";

export default function Lobby({ onJoin }: { onJoin: (code: string) => void }) {
  const [name, setName] = useState("");
  const [code, setCode] = useState("");
  const [error, setError] = useState("");
  const { setRoom } = useRoomStore();

  const createRoom = async () => {
    if (!name.trim()) return setError("Enter a display name");
    const res = await fetch("/room/create", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ display_name: name.trim() }),
    });
    const data = await res.json();
    localStorage.setItem("player_token", data.player_token);
    setRoom(data.code, data.player_token, name.trim());
    onJoin(data.code);
  };

  const joinRoom = async () => {
    if (!name.trim()) return setError("Enter a display name");
    if (!code.trim()) return setError("Enter a room code");
    const token = localStorage.getItem("player_token") || undefined;
    const res = await fetch("/room/join", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ code: code.trim().toUpperCase(), display_name: name.trim(), player_token: token }),
    });
    if (!res.ok) return setError("Room not found");
    const data = await res.json();
    localStorage.setItem("player_token", data.player_token);
    setRoom(code.trim().toUpperCase(), data.player_token, name.trim());
    onJoin(code.trim().toUpperCase());
  };

  return (
    <div className="min-h-screen bg-gray-900 flex items-center justify-center">
      <div className="bg-gray-800 p-8 rounded-xl w-96 space-y-4">
        <h1 className="text-2xl font-bold text-amber-400 text-center">вљ”пёЏ TTRPG Agent</h1>
        <input
          className="w-full p-3 rounded bg-gray-700 text-white placeholder-gray-400"
          placeholder="Your name"
          value={name}
          onChange={(e) => setName(e.target.value)}
        />
        <button onClick={createRoom} className="w-full p-3 bg-amber-600 hover:bg-amber-500 rounded text-white font-semibold">
          Create Room
        </button>
        <div className="flex gap-2">
          <input
            className="flex-1 p-3 rounded bg-gray-700 text-white placeholder-gray-400 uppercase"
            placeholder="Room code"
            maxLength={6}
            value={code}
            onChange={(e) => setCode(e.target.value.toUpperCase())}
          />
          <button onClick={joinRoom} className="p-3 bg-gray-600 hover:bg-gray-500 rounded text-white font-semibold">
            Join
          </button>
        </div>
        {error && <p className="text-red-400 text-sm">{error}</p>}
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Create RoomView.tsx**

```tsx
import { useWebSocket } from "../../hooks/useWebSocket";
import { useRoomStore } from "../../stores/roomStore";
import ChatWindow from "../Chat/ChatWindow";

export default function RoomView({ code, onLeave }: { code: string; onLeave: () => void }) {
  const { sendMessage } = useWebSocket(code);
  const { connected, reset } = useRoomStore();

  const handleLeave = () => {
    reset();
    onLeave();
  };

  return (
    <div className="min-h-screen bg-gray-900 flex flex-col">
      <header className="bg-gray-800 px-6 py-3 flex items-center justify-between border-b border-gray-700">
        <div className="flex items-center gap-3">
          <h1 className="text-amber-400 font-bold">вљ”пёЏ Room: {code}</h1>
          <span className={`text-xs px-2 py-1 rounded ${connected ? "bg-green-900 text-green-300" : "bg-red-900 text-red-300"}`}>
            {connected ? "Connected" : "Disconnected"}
          </span>
        </div>
        <button onClick={handleLeave} className="text-gray-400 hover:text-white text-sm">
          Leave
        </button>
      </header>
      <ChatWindow onSend={sendMessage} />
    </div>
  );
}
```

- [ ] **Step 3: Create ChatWindow.tsx**

```tsx
import MessageList from "./MessageList";
import MessageInput from "./MessageInput";

export default function ChatWindow({ onSend }: { onSend: (text: string) => void }) {
  return (
    <div className="flex-1 flex flex-col overflow-hidden">
      <MessageList />
      <MessageInput onSend={onSend} />
    </div>
  );
}
```

- [ ] **Step 4: Create MessageList.tsx**

```tsx
import { useEffect, useRef } from "react";
import { useChatStore } from "../../stores/chatStore";

export default function MessageList() {
  const { messages, streamingText, isStreaming } = useChatStore();
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages, streamingText]);

  return (
    <div className="flex-1 overflow-y-auto p-4 space-y-3">
      {messages.map((msg) => (
        <div key={msg.id} className={msg.sender === "player" ? "text-right" : ""}>
          {msg.sender === "system" ? (
            <p className="text-center text-sm text-gray-400 italic">{msg.text}</p>
          ) : msg.type === "dice_roll" ? (
            <p className="text-center text-sm text-purple-300 font-mono bg-purple-900/30 inline-block px-3 py-1 rounded">
              {msg.text}
            </p>
          ) : (
            <div className={`inline-block max-w-[80%] p-3 rounded-lg ${
              msg.sender === "player"
                ? "bg-blue-800 text-blue-100"
                : "bg-gray-700 text-gray-100"
            }`}>
              {msg.senderName && (
                <p className="text-xs text-gray-400 mb-1">{msg.senderName}</p>
              )}
              <p className="whitespace-pre-wrap">{msg.text}</p>
            </div>
          )}
        </div>
      ))}
      {isStreaming && (
        <div className="inline-block max-w-[80%] p-3 rounded-lg bg-gray-700 text-gray-100">
          <p className="text-xs text-amber-400 mb-1">Dungeon Master</p>
          <p className="whitespace-pre-wrap">{streamingText}<span className="animate-pulse">в–Љ</span></p>
        </div>
      )}
      <div ref={bottomRef} />
    </div>
  );
}
```

- [ ] **Step 5: Create MessageInput.tsx**

```tsx
import { useState } from "react";

export default function MessageInput({ onSend }: { onSend: (text: string) => void }) {
  const [text, setText] = useState("");

  const handleSend = () => {
    if (!text.trim()) return;
    onSend(text.trim());
    setText("");
  };

  return (
    <div className="p-4 bg-gray-800 border-t border-gray-700">
      <div className="flex gap-2">
        <input
          className="flex-1 p-3 rounded bg-gray-700 text-white placeholder-gray-400"
          placeholder="What do you do?"
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && handleSend()}
        />
        <button onClick={handleSend} className="px-6 py-3 bg-amber-600 hover:bg-amber-500 rounded text-white font-semibold">
          Send
        </button>
      </div>
    </div>
  );
}
```

- [ ] **Step 6: Create DiceIndicator.tsx (optional inline component)**

```tsx
export default function DiceIndicator({ result }: { result: string }) {
  return (
    <span className="inline-flex items-center gap-1 text-purple-300 font-mono text-sm bg-purple-900/30 px-2 py-1 rounded">
      рџЋІ {result}
    </span>
  );
}
```

- [ ] **Step 7: Commit**

```bash
git add frontend/src/components/
git commit -m "feat(frontend): Lobby, Room, and Chat UI components"
```

---

## Task 14: Integration Verification

- [ ] **Step 1: Create .env from example**

```bash
cp .env.example .env
# Edit .env: set LLM_API_KEY to a valid OpenRouter key
```

- [ ] **Step 2: Start all services**

```bash
docker compose up --build
```

Expected: All 3 services start. Backend on :8000, Frontend on :5173, Postgres on :5432.

- [ ] **Step 3: Run migrations**

```bash
docker compose exec backend uv run alembic upgrade head
```

- [ ] **Step 4: Verify end-to-end**

1. Open `http://localhost:5173`
2. Enter name в†’ Create Room в†’ get code
3. Type "I look around the tavern" в†’ DM streams response
4. Type "I try to pick the lock" в†’ DM calls skill_check в†’ dice result shows в†’ DM narrates

- [ ] **Step 5: Final commit**

```bash
git add -A
git commit -m "feat: Phase 0 complete вЂ” skeleton and DM core working end-to-end"
```
