import { useLogStore } from "./logs";
import { developmentLoggingEnabled } from "../loggingEnvironment";

const finishDisabledDebugTiming = () => {};

export function startDebugTiming(operation: string, detail: string): (outcome?: string) => void {
  if (!developmentLoggingEnabled()) return finishDisabledDebugTiming;
  const logs = useLogStore();
  const started = performance.now();
  logs.add("DEBUG", "performance", `${operation} started ${detail}`);
  return (outcome = "finished") => {
    logs.add("DEBUG", "performance",
      `${operation} ${outcome} ${detail} elapsed=${(performance.now() - started).toFixed(2)}ms`);
  };
}
