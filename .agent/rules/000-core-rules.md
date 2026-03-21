---
trigger: always_on
---

# Core Project Rules: Go-Gin & React-Vite

## Tech Stack
- Backend: Go (Golang) with Gin Web Framework.
- Frontend: React with Vite, TypeScript, and Tailwind CSS.

## High-Level Constraints
- **RAM Efficiency:** Avoid heavy dependencies. Use pointers in Go where beneficial, and avoid unnecessary re-renders in React.
- **LLM-Friendliness:** Use explicit, standard patterns. Favor "Boring Code" over clever abstractions so the AI can accurately predict and generate logic.
- **Project Structure:**
  - `/backend`: Go Gin source code.
  - `/frontend`: React Vite source code.