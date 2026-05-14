package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"portfolio-analysis/middleware"
	"portfolio-analysis/services/llm"
)

// ListThreads handles GET /api/v1/llm/threads
func (h *LLMHandler) ListThreads(c *gin.Context) {
	userHash := c.GetString(middleware.UserHashKey)

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if offset < 0 {
		offset = 0
	}

	// Use realUserHash to strip scenario prefixes so history is unified
	threads, total, err := h.LLM.ListThreads(llm.RealUserHash(userHash), limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load threads"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"threads": threads,
		"total":   total,
	})
}

// CreateThread handles POST /api/v1/llm/threads
func (h *LLMHandler) CreateThread(c *gin.Context) {
	userHash := c.GetString(middleware.UserHashKey)

	var req struct {
		Title string `json:"title"`
	}
	_ = c.ShouldBindJSON(&req)

	thread, err := h.LLM.CreateThread(llm.RealUserHash(userHash), req.Title)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create thread"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"thread": thread})
}

// GetThread handles GET /api/v1/llm/threads/:id
func (h *LLMHandler) GetThread(c *gin.Context) {
	userHash := c.GetString(middleware.UserHashKey)
	idStr := c.Param("id")
	threadID, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid thread ID"})
		return
	}

	thread, messages, err := h.LLM.GetThread(uint(threadID), llm.RealUserHash(userHash))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "thread not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load thread"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"thread":   thread,
		"messages": messages,
	})
}

// RenameThread handles PATCH /api/v1/llm/threads/:id
func (h *LLMHandler) RenameThread(c *gin.Context) {
	userHash := c.GetString(middleware.UserHashKey)
	idStr := c.Param("id")
	threadID, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid thread ID"})
		return
	}

	var req struct {
		Title string `json:"title" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "title is required"})
		return
	}

	if err := h.LLM.RenameThread(uint(threadID), llm.RealUserHash(userHash), req.Title); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// DeleteThread handles DELETE /api/v1/llm/threads/:id
func (h *LLMHandler) DeleteThread(c *gin.Context) {
	userHash := c.GetString(middleware.UserHashKey)
	idStr := c.Param("id")
	threadID, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid thread ID"})
		return
	}

	if err := h.LLM.DeleteThread(uint(threadID), llm.RealUserHash(userHash)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// SearchThreads handles GET /api/v1/llm/threads/search
func (h *LLMHandler) SearchThreads(c *gin.Context) {
	userHash := c.GetString(middleware.UserHashKey)
	query := c.Query("q")
	if query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "query parameter 'q' is required"})
		return
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	results, err := h.LLM.SearchThreads(llm.RealUserHash(userHash), query, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "search failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"results": results})
}
