import pytest

from app.services.voice import VoiceAudio, VoiceNotConfigured, VoiceService


class FakeSTT:
    async def transcribe(self, audio: bytes, mime_type: str) -> str:
        return f"{mime_type}:{len(audio)}"


class FakeTTS:
    async def synthesize(self, text: str) -> VoiceAudio:
        return VoiceAudio(text.encode("utf-8"))


@pytest.mark.asyncio
async def test_voice_service_uses_injected_providers():
    service = VoiceService(FakeSTT(), FakeTTS())

    assert await service.transcribe(b"abc", "audio/webm") == "audio/webm:3"
    audio = await service.synthesize("hello")
    assert audio.data == b"hello"


@pytest.mark.asyncio
async def test_voice_service_requires_configuration_by_default():
    with pytest.raises(VoiceNotConfigured):
        await VoiceService().transcribe(b"audio")
