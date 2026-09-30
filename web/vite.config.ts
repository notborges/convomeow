import { fileURLToPath } from "node:url";
import react from "@vitejs/plugin-react";
import { build, defineConfig, type Plugin } from "vite";

const workerEntry = fileURLToPath(
  new URL("./src/features/notifications/worker.ts", import.meta.url),
);

async function buildNotificationWorker(write: boolean) {
  const result = await build({
    configFile: false,
    build: {
      emptyOutDir: false,
      write,
      lib: {
        entry: workerEntry,
        formats: ["iife"],
        name: "ConvoMeowNotifications",
        fileName: () => "notifications-sw.js",
      },
    },
  });
  const bundle = Array.isArray(result) ? result[0] : result;
  if (!("output" in bundle)) throw new Error("Worker build returned a watcher");
  return bundle;
}

function notificationWorker(): Plugin {
  let development = false;
  return {
    name: "notification-worker",
    async closeBundle() {
      if (!development) await buildNotificationWorker(true);
    },
    configureServer(server) {
      development = true;
      server.middlewares.use("/app/notifications-sw.js", async (_req, res) => {
        try {
          const bundle = await buildNotificationWorker(false);
          const chunk = bundle.output.find((item) => item.type === "chunk");
          if (!chunk)
            throw new Error("Worker build did not produce JavaScript");
          res.setHeader("Content-Type", "text/javascript");
          res.setHeader("Cache-Control", "no-cache");
          res.end(chunk.code);
        } catch (error) {
          server.config.logger.error(String(error));
          res.statusCode = 500;
          res.end();
        }
      });
    },
  };
}

export default defineConfig({
  base: "/app/",
  plugins: [react(), notificationWorker()],
  server: {
    proxy: {
      "/api": { target: "http://127.0.0.1:8787", ws: true },
      "/app/session": "http://127.0.0.1:8787",
    },
  },
});
