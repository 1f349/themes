/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ["./*.go.html"],
  theme: {
    extend: {
      fontFamily: {
        ubuntu: ["var(--font-ubuntu)"],
        mono: ["var(--font-jetbrains-mono)"]
      },
      colors: {
        zinc: {
          50: "#F2F2F3",
          100: "#E4E6E7",
          200: "#C6C9CC",
          300: "#ACB0B4",
          400: "#91969C",
          500: "#747A81",
          600: "#5B6166",
          700: "#43474B",
          800: "#292B2E",
          900: "#111213",
          950: "#070808"
        }
      }
    }
  },
  plugins: [],
}
