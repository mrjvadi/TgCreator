import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// `npm run dev` proxies the API to a local `tgcreator panel` (port 8090).
export default defineConfig({
  plugins: [react()],
  server: { proxy: { "/api": "http://localhost:8090" } },
  build: { outDir: "dist", emptyOutDir: true, chunkSizeWarningLimit: 900 },
});
