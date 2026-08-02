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
