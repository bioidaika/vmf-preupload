import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  // Wails serves the compiled frontend from its own asset server. Keeping a
  // relative base also makes the same bundle useful as a browser preview.
  base: './',
})
