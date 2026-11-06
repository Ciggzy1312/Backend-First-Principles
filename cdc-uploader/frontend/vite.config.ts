import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
 
// The dev server runs on http://localhost:5173, which is the origin the Go
// server's CORS settings allow.
export default defineConfig({
  plugins: [react(), tailwindcss()],
});