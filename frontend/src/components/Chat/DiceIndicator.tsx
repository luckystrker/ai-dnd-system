export default function DiceIndicator({ result }: { result: string }) {
  return <span className="inline-flex rounded-md border border-violet-400/20 bg-violet-400/10 px-2 py-1 font-mono text-xs text-violet-200">{result}</span>;
}
