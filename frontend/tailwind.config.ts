import type { Config } from "tailwindcss";

const config: Config = {
  content: [
    "./src/app/**/*.{ts,tsx}",
    "./src/components/**/*.{ts,tsx}",
    "./src/hooks/**/*.{ts,tsx}",
  ],
  darkMode: "class",
  theme: {
    container: {
      center: true,
      padding: "1rem",
    },
    extend: {
      colors: {
        // Connection-pill semantics — referenced by name from ConnectionPill.tsx.
        pill: {
          connecting: "rgb(234 179 8)",
          ready: "rgb(34 197 94)",
          reconnecting: "rgb(249 115 22)",
          failed: "rgb(239 68 68)",
        },
      },
    },
  },
  plugins: [],
};

export default config;
