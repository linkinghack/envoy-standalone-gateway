import type {Config} from "tailwindcss";

export default {
  darkMode: ["class"],
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        ink: "rgb(var(--ink) / <alpha-value>)",
        canvas: "rgb(var(--canvas) / <alpha-value>)",
        paper: "rgb(var(--paper) / <alpha-value>)",
        line: "rgb(var(--line) / <alpha-value>)",
        signal: "rgb(var(--signal) / <alpha-value>)",
        healthy: "rgb(var(--healthy) / <alpha-value>)",
        muted: "rgb(var(--muted) / <alpha-value>)"
      },
      fontFamily: {
        sans: ["IBM Plex Sans Variable", "sans-serif"],
        display: ["Newsreader", "serif"],
        mono: ["IBM Plex Mono", "monospace"]
      },
      boxShadow: {
        panel: "0 18px 45px rgba(16, 22, 22, 0.08)",
        lift: "0 10px 24px rgba(11, 19, 18, 0.16)"
      },
      animation: {
        "signal-in": "signal-in .55s cubic-bezier(.22,1,.36,1) both"
      },
      keyframes: {
        "signal-in": {
          from: {opacity: "0", transform: "translateY(10px)"},
          to: {opacity: "1", transform: "translateY(0)"}
        }
      }
    }
  },
  plugins: []
} satisfies Config;
