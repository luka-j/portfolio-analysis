package llm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"google.golang.org/genai"

	"portfolio-analysis/models"
)

// ChatSearchResult represents a single matched thread.
type ChatSearchResult struct {
	ThreadID uint   `json:"thread_id"`
	Title    string `json:"title"`
	Snippet  string `json:"snippet,omitempty"`
}

// CreateThread creates a new empty thread for the given user.
func (s *Service) CreateThread(userHash, title string) (*models.ChatThread, error) {
	if title == "" {
		title = "New Conversation"
	}
	thread := &models.ChatThread{
		UserHash:  userHash,
		Title:     title,
		TurnCount: 0,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := s.DB.Create(thread).Error; err != nil {
		return nil, fmt.Errorf("creating thread: %w", err)
	}
	return thread, nil
}

// AppendMessage adds a message to the thread and updates its timestamp.
func (s *Service) AppendMessage(threadID uint, role, content string) error {
	msg := &models.ChatMessage{
		ThreadID:  threadID,
		Role:      role,
		Content:   content,
		CreatedAt: time.Now(),
	}

	tx := s.DB.Begin()
	if tx.Error != nil {
		return tx.Error
	}

	if err := tx.Create(msg).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("creating message: %w", err)
	}

	// Update the thread's updated_at and increment TurnCount if it's a complete turn (assistant response)
	updates := map[string]interface{}{
		"updated_at": time.Now(),
	}
	if role == "assistant" {
		updates["turn_count"] = s.DB.Raw("turn_count + 1")
	}

	if err := tx.Model(&models.ChatThread{}).Where("id = ?", threadID).Updates(updates).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("updating thread: %w", err)
	}

	return tx.Commit().Error
}

// GetThread loads a thread and its messages. Verifies ownership.
func (s *Service) GetThread(threadID uint, userHash string) (*models.ChatThread, []models.ChatMessage, error) {
	var thread models.ChatThread
	if err := s.DB.Where("id = ? AND user_hash = ?", threadID, userHash).First(&thread).Error; err != nil {
		return nil, nil, fmt.Errorf("finding thread: %w", err)
	}

	var messages []models.ChatMessage
	if err := s.DB.Where("thread_id = ?", threadID).Order("created_at ASC").Find(&messages).Error; err != nil {
		return nil, nil, fmt.Errorf("finding messages: %w", err)
	}

	return &thread, messages, nil
}

// ListThreads returns a paginated list of threads for a user.
func (s *Service) ListThreads(userHash string, limit, offset int) ([]models.ChatThread, int64, error) {
	var threads []models.ChatThread
	var total int64

	q := s.DB.Model(&models.ChatThread{}).Where("user_hash = ?", userHash)

	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("counting threads: %w", err)
	}

	if err := q.Order("updated_at DESC").Limit(limit).Offset(offset).Find(&threads).Error; err != nil {
		return nil, 0, fmt.Errorf("finding threads: %w", err)
	}

	return threads, total, nil
}

// SearchThreads searches titles first, then content.
func (s *Service) SearchThreads(userHash, query string, limit int) ([]ChatSearchResult, error) {
	var results []ChatSearchResult
	if query == "" {
		return results, nil
	}
	
	// Add % wildcards
	likeQuery := "%" + query + "%"

	// 1. Search by title
	var titleMatches []models.ChatThread
	if err := s.DB.Where("user_hash = ? AND title LIKE ?", userHash, likeQuery).
		Order("updated_at DESC").Limit(limit).Find(&titleMatches).Error; err != nil {
		return nil, fmt.Errorf("searching titles: %w", err)
	}

	matchedThreadIDs := make(map[uint]bool)
	for _, t := range titleMatches {
		results = append(results, ChatSearchResult{
			ThreadID: t.ID,
			Title:    t.Title,
		})
		matchedThreadIDs[t.ID] = true
	}

	// If we hit the limit just from titles, return early
	if len(results) >= limit {
		return results[:limit], nil
	}

	remainingLimit := limit - len(results)

	// 2. Search by content in messages
	// Join with threads to verify ownership and order by updated_at
	var contentMatches []struct {
		ThreadID uint
		Title    string
		Content  string
	}
	
	dbQuery := s.DB.Table("chat_messages").
		Select("chat_threads.id as thread_id, chat_threads.title, MIN(chat_messages.content) as content").
		Joins("JOIN chat_threads ON chat_threads.id = chat_messages.thread_id").
		Where("chat_threads.user_hash = ? AND chat_messages.content LIKE ?", userHash, likeQuery)
		
	if len(matchedThreadIDs) > 0 {
		var ids []uint
		for id := range matchedThreadIDs {
			ids = append(ids, id)
		}
		dbQuery = dbQuery.Where("chat_threads.id NOT IN ?", ids)
	}
	
	if err := dbQuery.Group("chat_threads.id, chat_threads.title").
		Order("MAX(chat_threads.updated_at) DESC").
		Limit(remainingLimit).
		Find(&contentMatches).Error; err != nil {
		return nil, fmt.Errorf("searching messages: %w", err)
	}

	for _, m := range contentMatches {
		// Create a snippet
		snippet := createSnippet(m.Content, query, 50)
		results = append(results, ChatSearchResult{
			ThreadID: m.ThreadID,
			Title:    m.Title,
			Snippet:  snippet,
		})
	}

	return results, nil
}

// createSnippet extracts a snippet of text around the first match of the query.
func createSnippet(content, query string, padding int) string {
	idx := strings.Index(strings.ToLower(content), strings.ToLower(query))
	if idx == -1 {
		if len(content) > padding*2 {
			return content[:padding*2] + "..."
		}
		return content
	}

	start := idx - padding
	if start < 0 {
		start = 0
	}
	
	end := idx + len(query) + padding
	if end > len(content) {
		end = len(content)
	}

	snippet := content[start:end]
	
	// Clean up newlines for the snippet
	snippet = strings.ReplaceAll(snippet, "\n", " ")
	
	if start > 0 {
		snippet = "..." + snippet
	}
	if end < len(content) {
		snippet = snippet + "..."
	}
	
	return snippet
}

// GenerateTitle async generates a title for a thread using the Gemini Flash model.
func (s *Service) GenerateTitle(ctx context.Context, messages []models.ChatMessage) (string, error) {
	if !s.isAvailable() {
		return "", ErrNotConfigured
	}

	// Send at most the first two messages (user + assistant) to keep the prompt small
	var contents []*genai.Content
	for i := 0; i < len(messages) && i < 2; i++ {
		role := genai.RoleUser
		if messages[i].Role == "assistant" {
			role = genai.RoleModel
		}
		contents = append(contents, &genai.Content{
			Role:  role,
			Parts: []*genai.Part{{Text: messages[i].Content}},
		})
	}

	prompt := "Generate a very short title (3-8 words, no quotes) that summarizes this conversation. Only output the title, nothing else."
	contents = append(contents, &genai.Content{
		Role:  genai.RoleUser,
		Parts: []*genai.Part{{Text: prompt}},
	})

	cfg := &genai.GenerateContentConfig{
		Temperature: genai.Ptr(float32(0.3)), // lower temp for more deterministic titles
	}

	title, err := s.callGemini(ctx, s.FlashModel, "title", contents, cfg)
	if err != nil {
		return "", fmt.Errorf("generating title: %w", err)
	}

	// Clean up any quotes the model might have added despite instructions
	title = strings.TrimSpace(title)
	title = strings.Trim(title, `"'`)

	return title, nil
}

// UpdateTitle updates the title of a thread.
func (s *Service) UpdateTitle(threadID uint, title string) error {
	if err := s.DB.Model(&models.ChatThread{}).Where("id = ?", threadID).Update("title", title).Error; err != nil {
		return fmt.Errorf("updating thread title: %w", err)
	}
	return nil
}

// RenameThread updates the thread title after ownership verification.
func (s *Service) RenameThread(threadID uint, userHash, newTitle string) error {
	res := s.DB.Model(&models.ChatThread{}).Where("id = ? AND user_hash = ?", threadID, userHash).Update("title", newTitle)
	if res.Error != nil {
		return fmt.Errorf("renaming thread: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("thread not found or permission denied")
	}
	return nil
}

// DeleteThread deletes a single thread and its messages.
func (s *Service) DeleteThread(threadID uint, userHash string) error {
	res := s.DB.Where("id = ? AND user_hash = ?", threadID, userHash).Delete(&models.ChatThread{})
	if res.Error != nil {
		return fmt.Errorf("deleting thread: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("thread not found or permission denied")
	}
	// Note: Messages are deleted automatically by the DB CASCADE constraint or GORM's association handling.
	return nil
}

// DeleteExpiredThreads deletes threads older than the retention period.
func (s *Service) DeleteExpiredThreads(retentionDays int) (int64, error) {
	if retentionDays <= 0 {
		return 0, nil
	}
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	res := s.DB.Where("updated_at < ?", cutoff).Delete(&models.ChatThread{})
	if res.Error != nil {
		return 0, fmt.Errorf("deleting expired threads: %w", res.Error)
	}
	return res.RowsAffected, nil
}
