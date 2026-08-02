import { useEffect, useRef, useState } from "react";


interface VoiceControlsProps {
  onAudio: (audio: Blob) => Promise<boolean>;
}


export default function VoiceControls({ onAudio }: VoiceControlsProps) {
  const recorderRef = useRef<MediaRecorder | null>(null);
  const streamRef = useRef<MediaStream | null>(null);
  const chunksRef = useRef<Blob[]>([]);
  const [recording, setRecording] = useState(false);
  const [error, setError] = useState(false);

  useEffect(() => () => {
    recorderRef.current?.stop();
    streamRef.current?.getTracks().forEach((track) => track.stop());
  }, []);

  const start = async () => {
    if (!navigator.mediaDevices?.getUserMedia || typeof MediaRecorder === "undefined") {
      setError(true);
      return;
    }
    let stream: MediaStream;
    try {
      stream = await navigator.mediaDevices.getUserMedia({ audio: true });
    } catch {
      setError(true);
      return;
    }
    setError(false);
    const recorder = new MediaRecorder(stream);
    streamRef.current = stream;
    recorderRef.current = recorder;
    chunksRef.current = [];
    recorder.ondataavailable = (event) => {
      if (event.data.size) {
        chunksRef.current.push(event.data);
      }
    };
    recorder.onstop = () => {
      const audio = new Blob(chunksRef.current, { type: recorder.mimeType || "audio/webm" });
      void onAudio(audio).catch(() => setError(true));
      stream.getTracks().forEach((track) => track.stop());
      streamRef.current = null;
      recorderRef.current = null;
    };
    recorder.start();
    setRecording(true);
  };

  const stop = () => {
    recorderRef.current?.stop();
    setRecording(false);
  };

  return (
    <button
      type="button"
      onClick={() => void (recording ? stop() : start())}
      className={`rounded-xl border px-3 py-2.5 text-sm transition ${recording ? "border-rose-300/50 bg-rose-400/10 text-rose-200" : "border-white/10 text-slate-400 hover:bg-white/5 hover:text-white"}`}
      title={recording ? "Stop recording" : "Use voice input"}
    >
      {recording ? "Stop" : error ? "Voice unavailable" : "Voice"}
    </button>
  );
}
