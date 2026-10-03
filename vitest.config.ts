import { defineConfig } from "vitest/config";
export default defineConfig({
  test: { include: ["sdk/ts/src/**/*.test.ts", "apps/web/src/**/*.test.ts"], testTimeout: 10000 },
});
