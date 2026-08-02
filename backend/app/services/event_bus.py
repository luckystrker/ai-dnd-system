from collections import defaultdict

from fastapi import WebSocket

from app.schemas.ws_messages import Event


class EventBus:
    def __init__(self) -> None:
        self._subscribers: dict[str, set[WebSocket]] = defaultdict(set)

    def subscribe(self, room_id: str, websocket: WebSocket) -> None:
        self._subscribers[room_id].add(websocket)

    def unsubscribe(self, room_id: str, websocket: WebSocket) -> None:
        subscribers = self._subscribers.get(room_id)
        if subscribers is None:
            return
        subscribers.discard(websocket)
        if not subscribers:
            self._subscribers.pop(room_id, None)

    async def publish(self, room_id: str, event: Event) -> None:
        message = event.model_dump_json()
        dead: list[WebSocket] = []
        for websocket in tuple(self._subscribers.get(room_id, ())):
            try:
                await websocket.send_text(message)
            except Exception:  # noqa: BLE001
                dead.append(websocket)
        for websocket in dead:
            self.unsubscribe(room_id, websocket)

    def subscriber_count(self, room_id: str) -> int:
        return len(self._subscribers.get(room_id, ()))


event_bus = EventBus()
