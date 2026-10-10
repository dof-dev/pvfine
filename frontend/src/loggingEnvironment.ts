/** build:dev 使用 development mode；Vite dev 和 Vitest 则通过 DEV 标识。 */
export function developmentLoggingEnabled(): boolean {
  return import.meta.env.DEV || import.meta.env.MODE === "development";
}
