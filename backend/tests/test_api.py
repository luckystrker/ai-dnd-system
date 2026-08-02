from fastapi.testclient import TestClient

from app.main import app


def test_health_endpoint():
    response = TestClient(app).get("/health")
    assert response.status_code == 200
    assert response.json() == {"status": "ok"}


def test_system_catalog_endpoint():
    client = TestClient(app)
    response = client.get("/systems")
    assert response.status_code == 200
    assert response.json()["systems"] == ["dnd5e", "story"]

    details = client.get("/systems/dnd5e")
    assert details.status_code == 200
    assert details.json()["id"] == "dnd5e"


def test_websocket_ping_and_join_event():
    with TestClient(app) as client, client.websocket_connect("/ws/room/ABC234?token=test-token") as websocket:
        joined = websocket.receive_json()
        assert joined["type"] == "player_joined"

        websocket.send_json({"type": "ping", "payload": {}})
        assert websocket.receive_json() == {"type": "pong", "payload": {}}
