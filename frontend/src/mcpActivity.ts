let active = false;
export function setMCPActive(value: boolean): void { active = value; }
export function assertMCPIdle(): void {
  if (active) throw new Error("AI 正在操作当前归档，请等待任务完成");
}
