package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"portfolio-analysis/middleware"
	"portfolio-analysis/models"
	"portfolio-analysis/services/llm"
)

func setupChatHistoryDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := fmt.Sprintf("file:chat_history_test_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(name), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.ChatThread{}, &models.ChatMessage{}))
	return db
}

func TestLLMHandler_ChatHistory(t *testing.T) {
	db := setupChatHistoryDB(t)

	llmSvc := &llm.Service{DB: db}

	handler := &LLMHandler{
		LLM: llmSvc,
		DB:  db,
	}

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(middleware.UserHashKey, "testuserhash")
		c.Next()
	})
	
	router.POST("/llm/threads", handler.CreateThread)
	router.GET("/llm/threads", handler.ListThreads)
	router.GET("/llm/threads/:id", handler.GetThread)
	router.PATCH("/llm/threads/:id", handler.RenameThread)
	router.DELETE("/llm/threads/:id", handler.DeleteThread)
	router.GET("/llm/threads/search", handler.SearchThreads)

	// 1. Create a thread
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/llm/threads", strings.NewReader(`{"title": "My API Thread"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusOK, w.Code)
	
	var createResp map[string]models.ChatThread
	err := json.Unmarshal(w.Body.Bytes(), &createResp)
	require.NoError(t, err)
	thread := createResp["thread"]
	assert.Equal(t, "My API Thread", thread.Title)
	threadID := thread.ID

	// 2. Append a message manually for testing
	err = llmSvc.AppendMessage(threadID, "user", "API message test")
	require.NoError(t, err)

	// 3. Get thread
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", fmt.Sprintf("/llm/threads/%d", threadID), nil)
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusOK, w.Code)
	var getResp struct {
		Thread   models.ChatThread    `json:"thread"`
		Messages []models.ChatMessage `json:"messages"`
	}
	err = json.Unmarshal(w.Body.Bytes(), &getResp)
	require.NoError(t, err)
	assert.Equal(t, "My API Thread", getResp.Thread.Title)
	assert.Len(t, getResp.Messages, 1)

	// 4. Rename thread
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PATCH", fmt.Sprintf("/llm/threads/%d", threadID), strings.NewReader(`{"title": "Renamed API Thread"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusOK, w.Code)
	
	// 5. Search thread
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/llm/threads/search?q=Renamed", nil)
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusOK, w.Code)
	var searchResp struct {
		Results []map[string]interface{} `json:"results"`
	}
	err = json.Unmarshal(w.Body.Bytes(), &searchResp)
	require.NoError(t, err)
	assert.Len(t, searchResp.Results, 1)

	// 6. Delete thread
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("DELETE", fmt.Sprintf("/llm/threads/%d", threadID), nil)
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusOK, w.Code)
	
	// 7. Verify deletion
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", fmt.Sprintf("/llm/threads/%d", threadID), nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}
