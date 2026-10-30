import { defineConfig, type Plugin } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import wails from "@wailsio/runtime/plugins/vite";

// The dev server injects component CSS as inline <style> elements, which the
// strict CSP in index.html blocks. Allow inline styles in dev mode only; the
// production build keeps the policy unchanged (see docs/dev-notes.md).
function devInlineStyles(): Plugin {
  const strict = "style-src 'self';";
  return {
    name: "smartnote-dev-inline-styles",
    apply: "serve",
    transformIndexHtml(html) {
      if (!html.includes(strict)) {
        throw new Error(`index.html CSP must contain "${strict}"`);
      }
      return html.replace(strict, "style-src 'self' 'unsafe-inline';");
    },
  };
}

// https://vitejs.dev/config/
export default defineConfig({
  server: {
    host: "127.0.0.1",
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
  },
  plugins: [svelte(), wails("./bindings"), devInlineStyles()],
});
