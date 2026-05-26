import { defineConfig } from "electron-vite";
import react from "@vitejs/plugin-react";
import { resolve } from "node:path";

const workbenchUiRoot = resolve(__dirname, "../browser/agent/packages/workbench-ui/src");

export default defineConfig({
  main: {
    resolve: {
      alias: {
        "@browseros/workbench-ui": workbenchUiRoot
      }
    },
    build: {
      rollupOptions: {
        external: ["node-pty"],
        input: {
          index: resolve(__dirname, "src/main/index.ts")
        }
      }
    }
  },
  preload: {
    resolve: {
      alias: {
        "@browseros/workbench-ui": workbenchUiRoot
      }
    },
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
    resolve: {
      alias: {
        "@browseros/workbench-ui": workbenchUiRoot
      },
      dedupe: ["react", "react-dom"]
    },
    build: {
      rollupOptions: {
        input: {
          index: resolve(__dirname, "index.html")
        }
      }
    }
  }
});
