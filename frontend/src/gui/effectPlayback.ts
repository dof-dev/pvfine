/** Deterministic visual approximation; does not execute ACT logic or combat. */
export interface ParticleMotion { life: number; frequency: number; maximum: number; maximumTime: number; gravity: number; position: number[]; area: number[]; alpha: number[]; velocity: number[]; acceleration: number[] }
export function effectFrameIndex(delays: number[], elapsed: number, loop: boolean): number {
  if (!delays.length) return -1;
  const safe = delays.map(delay => Math.max(16, Math.min(Number.isFinite(delay) ? delay : 100, 10000)));
  const duration = safe.reduce((sum, delay) => sum + delay, 0);
  let offset = loop ? Math.max(0, elapsed) % duration : Math.min(Math.max(0, elapsed), duration - 1);
  for (let i = 0; i < safe.length; i++) { offset -= safe[i]!; if (offset < 0) return i; }
  return safe.length - 1;
}
export function particleInstances(meta: ParticleMotion, elapsed: number): { x: number; y: number; alpha: number; age: number }[] {
  const life = Math.max(50, Math.min(meta.life, 60000)), frequency = meta.frequency > 0 ? Math.max(16, meta.frequency) : life;
  const count = Math.max(1, Math.min(64, meta.maximum)), cycle = Math.max(life + frequency * count, meta.maximumTime || 0);
  const time = Math.max(0, elapsed) % cycle;
  const result: { x: number; y: number; alpha: number; age: number }[] = [];
  for (let i = 0; i < count; i++) {
    const age = time - i * frequency;
    if (age < 0 || age >= life || (meta.maximumTime > 0 && i * frequency > meta.maximumTime)) continue;
    const seconds = age / 1000, ratio = age / life;
    const offset = (axis: number) => (meta.position[axis] ?? 0) + (meta.area[axis] ?? 0) * (Math.sin(i * 127.1 + axis * 311.7) * .5) + (meta.velocity[axis] ?? 0) * seconds + .5 * (meta.acceleration[axis] ?? 0) * seconds * seconds;
    result.push({ x: offset(0), y: offset(1) - offset(2) + .5 * meta.gravity * seconds * seconds, alpha: Math.max(0, Math.min(1, ((meta.alpha[0] ?? 100) + ((meta.alpha[1] ?? 100) - (meta.alpha[0] ?? 100)) * ratio) / 100)), age });
  }
  return result;
}
