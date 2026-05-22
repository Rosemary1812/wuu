import { defineConfig } from "electron-vite";
import react from "@vitejs/plugin-react";
import { resolve } from "node:path";

export default defineConfig({
  main: {
    build: {
      rollupOptions: {
        input: {
          index: resolve(__dirname, "src/main/index.ts")
        }
      }
    }
  },
  preload: {
    build: {
      rollupOptions: {
        input: {
          index: resolve(__dirname, "src/main/preload.ts")
        },
        output: {
          format: "cjs"
        }
      }
    }
  },
  renderer: {
    root: ".",
    plugins: [react()],
    build: {
      rollupOptions: {
        input: {
          index: resolve(__dirname, "index.html")
        }
      }
    }
  }
});
