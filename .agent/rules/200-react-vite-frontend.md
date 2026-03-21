---
trigger: glob
globs: **/*.{tsx,ts,jsx,js}
---

# React Vite Frontend Guidelines

## Performance & RAM
- **Direct Imports:** Never use barrel files (e.g., `import { X, Y } from './components'`). Import from the specific file to keep the Vite dev server light.
- **State Management:** Use `useState` and `useContext` for global state. Avoid Redux or heavy state machines unless strictly required.
- **Atomic Components:** Keep components under 150 lines.

## LLM Optimization
- **Functional Components:** Always use `export function Name() {}` instead of arrow functions for better AI recognition of component boundaries.
- **Strict TypeScript:** No `any` types. Define interfaces for all component props.