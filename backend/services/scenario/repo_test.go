package scenario

import (
	"errors"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"portfolio-analysis/models"
)

// setupTestDB helper initializing in-memory DB for repo testing
func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open in-memory sqlite db: %v", err)
	}

	// AutoMigrate all models related to scenario
	err = db.AutoMigrate(&models.ScenarioRecord{}, &models.User{})
	if err != nil {
		t.Fatalf("failed to auto-migrate: %v", err)
	}

	return db
}

func TestRepositoryCRUD(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRepository(db)

	userID := uint(1)
	spec := ScenarioSpec{
		Base: BaseModeReal,
	}

	t.Run("Create and Get Success", func(t *testing.T) {
		record, err := repo.Create(userID, spec, "Test Scenario", true)
		if err != nil {
			t.Fatalf("unexpected error creating scenario: %v", err)
		}
		if record.ID == 0 {
			t.Error("expected non-zero ID")
		}

		fetched, err := repo.Get(userID, record.ID)
		if err != nil {
			t.Fatalf("unexpected error getting scenario: %v", err)
		}
		if fetched == nil {
			t.Fatal("expected to fetch scenario, got nil")
		}
		if fetched.Name != "Test Scenario" {
			t.Errorf("expected name 'Test Scenario', got %q", fetched.Name)
		}

		parsed, err := ParseSpec(fetched)
		if err != nil {
			t.Fatalf("failed to parse spec: %v", err)
		}
		if parsed.Base != BaseModeReal {
			t.Errorf("expected BaseModeReal, got %v", parsed.Base)
		}
	})

	t.Run("Create duplicate name fails", func(t *testing.T) {
		_, err := repo.Create(userID, spec, "Test Scenario", false)
		if !errors.Is(err, ErrNameExists) {
			t.Errorf("expected ErrNameExists, got %v", err)
		}
	})

	t.Run("Get non-existent yields nil", func(t *testing.T) {
		res, err := repo.Get(userID, 9999)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res != nil {
			t.Errorf("expected nil for missing scenario, got %v", res)
		}
	})

	t.Run("List scenarios", func(t *testing.T) {
		// Create another one
		_, err := repo.Create(userID, spec, "Another Scenario", false)
		if err != nil {
			t.Fatalf("failed to create: %v", err)
		}

		list, err := repo.List(userID)
		if err != nil {
			t.Fatalf("failed to list: %v", err)
		}
		if len(list) != 2 {
			t.Errorf("expected 2 scenarios in list, got %d", len(list))
		}
	})

	t.Run("Update scenario name, pinned state, spec", func(t *testing.T) {
		record, _ := repo.Create(userID, spec, "To Update", false)

		newName := "Updated Name"
		newPinned := true
		newSpec := ScenarioSpec{Base: BaseModeEmpty}

		patch := ScenarioPatch{
			Name:   &newName,
			Pinned: &newPinned,
			Spec:   &newSpec,
		}

		updated, err := repo.Update(userID, record.ID, patch)
		if err != nil {
			t.Fatalf("unexpected update error: %v", err)
		}
		if updated.Name != newName {
			t.Errorf("expected updated name, got %q", updated.Name)
		}
		if !updated.Pinned {
			t.Error("expected pinned to be true")
		}

		parsed, _ := ParseSpec(updated)
		if parsed.Base != BaseModeEmpty {
			t.Errorf("expected updated base to be empty, got %v", parsed.Base)
		}

		// Update to duplicate name should fail
		dupName := "Test Scenario"
		_, err = repo.Update(userID, record.ID, ScenarioPatch{Name: &dupName})
		if !errors.Is(err, ErrNameExists) {
			t.Errorf("expected duplicate name update error, got %v", err)
		}

		// Update non-existent returns nil
		res, err := repo.Update(userID, 9999, patch)
		if err != nil {
			t.Fatalf("unexpected error updating missing: %v", err)
		}
		if res != nil {
			t.Errorf("expected nil for updating non-existent, got %v", res)
		}
	})

	t.Run("Delete scenario", func(t *testing.T) {
		record, _ := repo.Create(userID, spec, "To Delete", false)

		err := repo.Delete(userID, record.ID)
		if err != nil {
			t.Fatalf("delete failed: %v", err)
		}

		fetched, _ := repo.Get(userID, record.ID)
		if fetched != nil {
			t.Error("expected record to be deleted")
		}
	})

	t.Run("Touch last used", func(t *testing.T) {
		record, _ := repo.Create(userID, spec, "Touch Test", false)
		time.Sleep(10 * time.Millisecond) // ensure time passes slightly
		oldTime := record.LastUsedAt

		repo.TouchLastUsed(userID, record.ID)

		updated, _ := repo.Get(userID, record.ID)
		if !updated.LastUsedAt.After(oldTime) {
			t.Errorf("expected last_used_at to be touched and be after %v, got %v", oldTime, updated.LastUsedAt)
		}
	})

	t.Run("Evict stale unpinned scenarios", func(t *testing.T) {
		// Clean up existing
		db.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&models.ScenarioRecord{})

		// Create pinned scenario (should never evict)
		_, _ = repo.Create(userID, spec, "Pinned Keep", true)

		// Create unpinned fresh scenario
		_, _ = repo.Create(userID, spec, "Unpinned Fresh", false)

		// Create unpinned stale scenario by manually modifying last_used_at in DB
		stale, _ := repo.Create(userID, spec, "Unpinned Stale", false)
		db.Model(stale).Update("last_used_at", time.Now().UTC().Add(-2 * time.Hour))

		// Evict scenarios older than 1 hour
		threshold := time.Now().UTC().Add(-1 * time.Hour)
		affected, evictedLabels, err := repo.EvictStaleUnpinned(threshold)
		if err != nil {
			t.Fatalf("failed eviction: %v", err)
		}

		if affected != 1 {
			t.Errorf("expected 1 record to be evicted, got %d", affected)
		}
		if len(evictedLabels) != 1 {
			t.Errorf("expected 1 eviction label, got %d", len(evictedLabels))
		}

		list, _ := repo.List(userID)
		if len(list) != 2 {
			t.Errorf("expected 2 scenarios remaining, got %d", len(list))
		}
	})
}
