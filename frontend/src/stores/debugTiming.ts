import { useLogStore } from "./logs";

export function startDebugTiming(operation: string, detail: string): (outcome?: string) => void {
  const logs = useLogStore();
  const started = performance.now();
  logs.add("DEBUG", "performance", `${operation} started ${detail}`);
  return (outcome = "finished") => {
    logs.add("DEBUG", "performance",
      `${operation} ${outcome} ${detail} elapsed=${(performance.now() - started).toFixed(2)}ms`);
  };
}
