import { defineConfig } from "vite";
export default defineConfig({
  build: {
    target: "node24",
    sourcemap: true,
    lib: { entry: "src/main.ts", formats: ["es"], fileName: () => "main.mjs" },
    rolldownOptions: { external: [/^node:/, "ws"] },
  },
});
