from dataclasses import dataclass
from typing import Protocol


class VoiceNotConfigured(RuntimeError):
    pass


class SpeechToTextProvider(Protocol):
    async def transcribe(self, audio: bytes, mime_type: str) -> str: ...


class TextToSpeechProvider(Protocol):
    async def synthesize(self, text: str) -> "VoiceAudio": ...


@dataclass(frozen=True)
class VoiceAudio:
    data: bytes
    mime_type: str = "audio/mpeg"


class NullSpeechToText:
    async def transcribe(self, _audio: bytes, _mime_type: str) -> str:
        raise VoiceNotConfigured("Speech-to-text provider is not configured")


class NullTextToSpeech:
    async def synthesize(self, _text: str) -> VoiceAudio:
        raise VoiceNotConfigured("Text-to-speech provider is not configured")


class VoiceService:
    def __init__(
        self,
        stt: SpeechToTextProvider | None = None,
        tts: TextToSpeechProvider | None = None,
    ) -> None:
        self.stt = stt or NullSpeechToText()
        self.tts = tts or NullTextToSpeech()

    async def transcribe(self, audio: bytes, mime_type: str = "audio/webm") -> str:
        if not audio:
            raise ValueError("Audio payload is empty")
        return await self.stt.transcribe(audio, mime_type)

    async def synthesize(self, text: str) -> VoiceAudio:
        if not text.strip():
            raise ValueError("Text payload is empty")
        return await self.tts.synthesize(text)


voice_service = VoiceService()
