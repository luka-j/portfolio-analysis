---
trigger: glob
globs: **/*.go
---

# Go Gin Backend Guidelines

## Performance & RAM
- **JSON Handling:** Use `gin.Context.JSON()` but prefer defining fixed structs for requests/responses to minimize reflection overhead.
- **No Global State:** Always use dependency injection (e.g., passing DB handles to controllers via structs).
- **Middleware:** Keep middleware reasonably small.
- **Error Handling:** Use explicit error checking (`if err != nil`). Wrap errors with `fmt.Errorf("context: %w", err)` for traceability.

## LLM Optimization
- **Standard Signatures:** Always use the standard `func(c *gin.Context)` for handlers.
- **Documentation:** Every exported function must have a one-line comment starting with the function name.