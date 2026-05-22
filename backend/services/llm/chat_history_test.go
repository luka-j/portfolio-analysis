package llm

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"portfolio-analysis/models"
)

func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	err = db.AutoMigrate(&models.ChatThread{}, &models.ChatMessage{})
	require.NoError(t, err)
	return db
}

func TestChatHistoryService_CRUD(t *testing.T) {
	db := setupTestDB(t)
	svc := &Service{DB: db}

	userHash := "user123"

	// 1. Create thread
	thread, err := svc.CreateThread(userHash, "Test Title")
	require.NoError(t, err)
	assert.NotZero(t, thread.ID)
	assert.Equal(t, "Test Title", thread.Title)
	assert.Equal(t, 0, thread.TurnCount)

	// 2. Append messages
	err = svc.AppendMessage(thread.ID, "user", "Hello world")
	require.NoError(t, err)

	// Update timestamp simulating a delay
	time.Sleep(10 * time.Millisecond)

	err = svc.AppendMessage(thread.ID, "assistant", "Hello back")
	require.NoError(t, err)

	// 3. Get Thread
	loaded, msgs, err := svc.GetThread(thread.ID, userHash)
	require.NoError(t, err)
	assert.Equal(t, 1, loaded.TurnCount)
	assert.Len(t, msgs, 2)
	assert.Equal(t, "user", msgs[0].Role)
	assert.Equal(t, "assistant", msgs[1].Role)

	// 4. Update title
	err = svc.UpdateTitle(thread.ID, "New Title")
	require.NoError(t, err)
	
	loaded, _, _ = svc.GetThread(thread.ID, userHash)
	assert.Equal(t, "New Title", loaded.Title)

	// Rename via endpoint logic
	err = svc.RenameThread(thread.ID, userHash, "Renamed Title")
	require.NoError(t, err)
	loaded, _, _ = svc.GetThread(thread.ID, userHash)
	assert.Equal(t, "Renamed Title", loaded.Title)

	// 5. Delete thread
	err = svc.DeleteThread(thread.ID, userHash)
	require.NoError(t, err)

	_, _, err = svc.GetThread(thread.ID, userHash)
	assert.Error(t, err) // Should not be found
}

func TestChatHistoryService_ListAndSearch(t *testing.T) {
	db := setupTestDB(t)
	svc := &Service{DB: db}
	userHash := "user_search"

	// Create a few threads
	t1, _ := svc.CreateThread(userHash, "Apple earnings report")
	svc.AppendMessage(t1.ID, "user", "What were Apple's earnings?")
	time.Sleep(10 * time.Millisecond) // ensure ordering

	t2, _ := svc.CreateThread(userHash, "Microsoft and AI")
	svc.AppendMessage(t2.ID, "user", "How is Microsoft using AI?")
	svc.AppendMessage(t2.ID, "assistant", "They partnered with OpenAI and integrated it into Bing and Office.")
	time.Sleep(10 * time.Millisecond)

	t3, _ := svc.CreateThread(userHash, "Portfolio risk")
	svc.AppendMessage(t3.ID, "user", "Analyze my risk")
	svc.AppendMessage(t3.ID, "assistant", "Your risk is high due to tech exposure, especially Apple.")

	// List
	threads, total, err := svc.ListThreads(userHash, 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	assert.Len(t, threads, 3)
	// Order should be descending by updated_at (t3, t2, t1)
	assert.Equal(t, t3.ID, threads[0].ID)

	// Search - title match
	res, err := svc.SearchThreads(userHash, "Apple", 5)
	require.NoError(t, err)
	assert.Len(t, res, 2)
	// t1 matches title, t3 matches content. Title matches should come first.
	assert.Equal(t, t1.ID, res[0].ThreadID)
	assert.Equal(t, t3.ID, res[1].ThreadID)
	assert.NotEmpty(t, res[1].Snippet)

	// Search - content match only
	res, err = svc.SearchThreads(userHash, "Bing", 5)
	require.NoError(t, err)
	assert.Len(t, res, 1)
	assert.Equal(t, t2.ID, res[0].ThreadID)
}

func TestChatHistoryService_Retention(t *testing.T) {
	db := setupTestDB(t)
	svc := &Service{DB: db}

	t1, _ := svc.CreateThread("user1", "Old Thread")
	db.Model(t1).Update("updated_at", time.Now().AddDate(0, 0, -400)) // 400 days old

	t2, _ := svc.CreateThread("user1", "New Thread")
	db.Model(t2).Update("updated_at", time.Now().AddDate(0, 0, -10)) // 10 days old

	deleted, err := svc.DeleteExpiredThreads(365)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)

	_, _, err = svc.GetThread(t1.ID, "user1")
	assert.Error(t, err) // t1 deleted

	_, _, err = svc.GetThread(t2.ID, "user1")
	require.NoError(t, err) // t2 remains
}

func TestCreateSnippet(t *testing.T) {
	content := "This is a very long text that goes on and on and contains a specific keyword we are looking for. After the keyword, it continues some more."
	query := "specific keyword"
	
	snippet := createSnippet(content, query, 15)
	// Should include ~15 chars before and after
	assert.Contains(t, snippet, "specific keyword")
	assert.True(t, strings.HasPrefix(snippet, "..."))
	assert.True(t, strings.HasSuffix(snippet, "..."))
}

// TestChatHistoryService_EdgeCases covers all remaining error and boundary conditions in the chat history.
func TestChatHistoryService_EdgeCases(t *testing.T) {
	db := setupTestDB(t)
	svc := &Service{DB: db}
	userHash := "user_edges"

	// 1. Create thread with empty title
	t1, err := svc.CreateThread(userHash, "")
	require.NoError(t, err)
	assert.Equal(t, "New Conversation", t1.Title)

	// 2. RenameThread not found or permission denied
	err = svc.RenameThread(999, userHash, "New Title")
	assert.ErrorContains(t, err, "thread not found or permission denied")

	// 3. DeleteThread not found or permission denied
	err = svc.DeleteThread(999, userHash)
	assert.ErrorContains(t, err, "thread not found or permission denied")

	// 4. DeleteExpiredThreads with non-positive retentionDays
	deleted, err := svc.DeleteExpiredThreads(0)
	require.NoError(t, err)
	assert.Equal(t, int64(0), deleted)

	deleted, err = svc.DeleteExpiredThreads(-10)
	require.NoError(t, err)
	assert.Equal(t, int64(0), deleted)

	// 5. SearchThreads with empty query
	res, err := svc.SearchThreads(userHash, "", 5)
	require.NoError(t, err)
	assert.Empty(t, res)

	// 6. createSnippet when query is not found and content is long
	snippet := createSnippet("This is a very long content string that exceeds twice the padding size, so it will get truncated.", "nonexistent", 5)
	assert.True(t, strings.HasSuffix(snippet, "..."))
}
