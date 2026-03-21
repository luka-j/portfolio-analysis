---
trigger: manual
---

# Workflow: Verify Changes
1. Run `go fmt ./...` in the backend folder.
2. Run `go test ./...` to ensure no regressions.
3. Check for any "TODO" comments left in the code.
4. Run `npm run lint` in the frontend folder.
5. Report any memory-heavy patterns or unoptimized imports found.