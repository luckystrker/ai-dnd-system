import asyncio
import base64
import json
import logging
import uuid
from typing import Any

from fastapi import APIRouter, WebSocket, WebSocketDisconnect

from app.agents.dm_agent import run_dm_agent
from app.db import async_session_factory
from app.memory.campaign import build_dm_context
from app.memory.visibility import Visibility
from app.schemas.room import BoardToken
from app.schemas.ws_messages import Event
from app.services.board import update_board
from app.services.character_manager import find_room
from app.services.event_bus import event_bus
from app.services.event_store import append_event
from app.services.voice import VoiceNotConfigured, voice_service

router = APIRouter()
logger = logging.getLogger(__name__)
_room_queues: dict[str, asyncio.Queue[str]] = {}
_room_workers: dict[str, asyncio.Task[None]] = {}
_persistence_tasks: set[asyncio.Task[None]] = set()


async def _persist_room_event(
    room_id: str,
    event_type: str,
    payload: dict[str, Any],
    *,
    visibility: Visibility | str = Visibility.PARTY,
    tags: tuple[str, ...] = (),
    database_room_id: str | None = None,
) -> None:
    try:
        async with async_session_factory() as session:
            room_uuid: uuid.UUID
            try:
                room_uuid = uuid.UUID(database_room_id or room_id)
            except ValueError:
                room = await find_room(session, room_id)
                if room is None:
                    return
                room_uuid = room.id
            await append_event(
                session,
                room_uuid,
                event_type,
                payload,
                visibility=visibility,
                tags=tags,
                publish=False,
            )
    except Exception:
        logger.warning("Could not persist room event", exc_info=True)


def _schedule_persistence(coroutine) -> None:
    task = asyncio.create_task(coroutine)
    _persistence_tasks.add(task)
    task.add_done_callback(_persistence_tasks.discard)


async def _resolve_room_id(room_code: str) -> str | None:
    try:
        return str(uuid.UUID(room_code))
    except ValueError:
        pass
    async with async_session_factory() as session:
        room = await find_room(session, room_code)
        return str(room.id) if room else None


async def _process_queue(room_id: str, queue: asyncio.Queue[str]) -> None:
    while True:
        message_text = await queue.get()
        try:
            context_text = None
            database_room_id = await _resolve_room_id(room_id)
            try:
                if database_room_id:
                    async with async_session_factory() as session:
                        context = await build_dm_context(
                            session, database_room_id, message_text
                        )
                        context_text = context.render()
            except Exception:
                logger.warning("Could not build DM context", exc_info=True)

            response = await run_dm_agent(
                room_id,
                message_text,
                context_text=context_text,
                database_room_id=database_room_id,
            )
            if response:
                event_type = (
                    "journal"
                    if message_text.strip().lower() == "/end_session"
                    else "dm_response"
                )
                payload = (
                    {"summary": response}
                    if event_type == "journal"
                    else {"text": response}
                )
                _schedule_persistence(
                    _persist_room_event(
                        room_id,
                        event_type,
                        payload,
                        visibility=Visibility.PARTY,
                        tags=("journal",)
                        if event_type == "journal"
                        else ("narrative",),
                        database_room_id=database_room_id,
                    )
                )
                if event_type == "journal":
                    logger.info("Session journal created for room %s", room_id)
                try:
                    audio = await voice_service.synthesize(response)
                    await event_bus.publish(
                        room_id,
                        Event(
                            type="voice_audio",
                            payload={
                                "data": base64.b64encode(audio.data).decode("ascii"),
                                "mime_type": audio.mime_type,
                            },
                        ),
                    )
                except VoiceNotConfigured:
                    pass
                except Exception:
                    logger.warning("Could not synthesize DM response", exc_info=True)
        finally:
            queue.task_done()


def _ensure_room_worker(room_id: str) -> asyncio.Queue[str]:
    queue = _room_queues.setdefault(room_id, asyncio.Queue())
    worker = _room_workers.get(room_id)
    if worker is None or worker.done():
        _room_workers[room_id] = asyncio.create_task(_process_queue(room_id, queue))
    return queue


async def stop_room_workers() -> None:
    workers = tuple(_room_workers.values())
    for worker in workers:
        worker.cancel()
    if workers:
        await asyncio.gather(*workers, return_exceptions=True)
    persistence_tasks = tuple(_persistence_tasks)
    for task in persistence_tasks:
        task.cancel()
    if persistence_tasks:
        await asyncio.gather(*persistence_tasks, return_exceptions=True)
    _persistence_tasks.clear()
    _room_workers.clear()
    _room_queues.clear()


def _message_text(data: dict[str, Any]) -> str:
    payload = data.get("payload")
    if not isinstance(payload, dict):
        return ""
    text = payload.get("text")
    return text.strip() if isinstance(text, str) else ""


async def _move_board_token(room_id: str, payload: dict[str, Any]) -> None:
    token_id = payload.get("token_id")
    if not isinstance(token_id, str) or not token_id.strip():
        raise ValueError("board_move requires token_id")
    token = BoardToken.model_validate(payload)
    async with async_session_factory() as session:
        room = await find_room(session, room_id)
        if room is None:
            raise ValueError("Room not found")
        await update_board(session, room.id, tokens={token_id: token.model_dump()})


@router.websocket("/ws/room/{code}")
async def websocket_room(websocket: WebSocket, code: str, token: str = "") -> None:
    await websocket.accept()
    room_id = code.strip().upper()
    queue = _ensure_room_worker(room_id)
    event_bus.subscribe(room_id, websocket)

    await event_bus.publish(
        room_id,
        Event(
            type="player_joined",
            payload={"name": f"Player ({token[:8]}...)" if token else "Player"},
        ),
    )

    try:
        while True:
            try:
                data = json.loads(await websocket.receive_text())
            except json.JSONDecodeError:
                await event_bus.publish(
                    room_id,
                    Event(type="error", payload={"message": "Invalid JSON message"}),
                )
                continue

            if not isinstance(data, dict):
                continue
            message_type = data.get("type")

            if message_type == "ping":
                await websocket.send_text(json.dumps({"type": "pong", "payload": {}}))
            elif message_type == "player_message":
                text = _message_text(data)
                if not text:
                    continue
                if len(text) > 4000:
                    await websocket.send_text(
                        json.dumps(
                            {
                                "type": "error",
                                "payload": {"message": "Message is too long"},
                            }
                        )
                    )
                    continue
                await event_bus.publish(
                    room_id,
                    Event(
                        type="player_message",
                        payload={"sender": "player", "text": text},
                        sender_id=token[:8] or None,
                    ),
                )
                _schedule_persistence(
                    _persist_room_event(
                        room_id,
                        "player_action",
                        {"text": text, "sender_id": token[:8] or None},
                        visibility=Visibility.PARTY,
                        tags=("player_action",),
                    )
                )
                await queue.put(text)
            elif message_type == "board_move":
                payload = data.get("payload")
                if not isinstance(payload, dict):
                    continue
                try:
                    await _move_board_token(room_id, payload)
                except Exception as error:  # noqa: BLE001
                    await websocket.send_text(
                        json.dumps(
                            {"type": "error", "payload": {"message": str(error)}}
                        )
                    )
            elif message_type == "voice_input":
                payload = data.get("payload")
                if not isinstance(payload, dict) or not isinstance(
                    payload.get("audio_base64"), str
                ):
                    await websocket.send_text(
                        json.dumps(
                            {
                                "type": "error",
                                "payload": {
                                    "message": "voice_input requires audio_base64"
                                },
                            }
                        )
                    )
                    continue
                try:
                    audio = base64.b64decode(payload["audio_base64"], validate=True)
                    if len(audio) > 10 * 1024 * 1024:
                        raise ValueError("Audio payload is too large")
                    mime_type = payload.get("mime_type", "audio/webm")
                    if not isinstance(mime_type, str):
                        mime_type = "audio/webm"
                    transcript = await voice_service.transcribe(audio, mime_type)
                    await event_bus.publish(
                        room_id,
                        Event(
                            type="voice_transcript",
                            payload={"text": transcript},
                            sender_id=token[:8] or None,
                        ),
                    )
                    await queue.put(transcript)
                except VoiceNotConfigured as error:
                    await websocket.send_text(
                        json.dumps(
                            {"type": "error", "payload": {"message": str(error)}}
                        )
                    )
                except Exception as error:  # noqa: BLE001
                    await websocket.send_text(
                        json.dumps(
                            {
                                "type": "error",
                                "payload": {"message": f"Voice input failed: {error}"},
                            }
                        )
                    )
            else:
                await websocket.send_text(
                    json.dumps(
                        {
                            "type": "error",
                            "payload": {"message": "Unknown message type"},
                        }
                    )
                )
    except WebSocketDisconnect:
        pass
    finally:
        event_bus.unsubscribe(room_id, websocket)
