package scenario

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"portfolio-analysis/models"
)

// Repository provides CRUD operations for ScenarioRecord rows, always scoped to a user.
type Repository struct {
	DB *gorm.DB
}

// NewRepository creates a scenario Repository backed by the given GORM database.
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{DB: db}
}

// ScenarioSummary is returned by List — enough to populate the picker UI without
// sending the full spec JSON over the wire for every row.
type ScenarioSummary struct {
	ID         uint      `json:"id"`
	Name       string    `json:"name"`
	Pinned     bool      `json:"pinned"`
	CreatedAt  time.Time `json:"created_at"`
	LastUsedAt time.Time `json:"last_used_at"`
}

// Create persists a new scenario and returns the saved row.
func (r *Repository) Create(userID uint, spec ScenarioSpec, name string, pinned bool) (*models.ScenarioRecord, error) {
	specJSON, err := json.Marshal(spec)
	if err != nil {
		return nil, fmt.Errorf("marshalling spec: %w", err)
	}
	now := time.Now().UTC()
	row := &models.ScenarioRecord{
		UserID:     userID,
		Name:       name,
		Pinned:     pinned,
		SpecJSON:   string(specJSON),
		LastUsedAt: now,
	}
	if err := r.DB.Create(row).Error; err != nil {
		return nil, fmt.Errorf("creating scenario: %w", err)
	}
	return row, nil
}

// Get returns a single ScenarioRecord owned by userID. Returns nil, nil when not found.
func (r *Repository) Get(userID, id uint) (*models.ScenarioRecord, error) {
	var row models.ScenarioRecord
	err := r.DB.Where("id = ? AND user_id = ?", id, userID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fetching scenario %d: %w", id, err)
	}
	return &row, nil
}

// List returns all scenarios owned by userID, ordered newest first.
func (r *Repository) List(userID uint) ([]ScenarioSummary, error) {
	var rows []models.ScenarioRecord
	if err := r.DB.Where("user_id = ?", userID).Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("listing scenarios: %w", err)
	}
	summaries := make([]ScenarioSummary, len(rows))
	for i, row := range rows {
		summaries[i] = ScenarioSummary{
			ID:         row.ID,
			Name:       row.Name,
			Pinned:     row.Pinned,
			CreatedAt:  row.CreatedAt,
			LastUsedAt: row.LastUsedAt,
		}
	}
	return summaries, nil
}

// ScenarioPatch contains the fields that can be updated on an existing scenario.
type ScenarioPatch struct {
	Name   *string      `json:"name,omitempty"`
	Pinned *bool        `json:"pinned,omitempty"`
	Spec   *ScenarioSpec `json:"spec,omitempty"`
}

// Update applies a partial update to an existing scenario. Only non-nil fields are changed.
func (r *Repository) Update(userID, id uint, patch ScenarioPatch) (*models.ScenarioRecord, error) {
	row, err := r.Get(userID, id)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, nil
	}
	updates := map[string]interface{}{"updated_at": time.Now().UTC()}
	if patch.Name != nil {
		updates["name"] = *patch.Name
	}
	if patch.Pinned != nil {
		updates["pinned"] = *patch.Pinned
	}
	if patch.Spec != nil {
		specJSON, err := json.Marshal(*patch.Spec)
		if err != nil {
			return nil, fmt.Errorf("marshalling spec: %w", err)
		}
		updates["spec_json"] = string(specJSON)
	}
	if err := r.DB.Model(row).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("updating scenario %d: %w", id, err)
	}
	return r.Get(userID, id)
}

// Delete removes a scenario owned by userID. No-op when the row does not exist.
func (r *Repository) Delete(userID, id uint) error {
	if err := r.DB.Where("id = ? AND user_id = ?", id, userID).Delete(&models.ScenarioRecord{}).Error; err != nil {
		return fmt.Errorf("deleting scenario %d: %w", id, err)
	}
	return nil
}

// TouchLastUsed updates LastUsedAt to now, extending the eviction window.
func (r *Repository) TouchLastUsed(userID, id uint) {
	r.DB.Model(&models.ScenarioRecord{}).
		Where("id = ? AND user_id = ?", id, userID).
		Update("last_used_at", time.Now().UTC())
}

// EvictStaleUnpinned deletes unpinned scenarios not used since olderThan.
// Returns the number of rows deleted and the names/IDs of deleted rows so callers can log them.
func (r *Repository) EvictStaleUnpinned(olderThan time.Time) (int64, []string, error) {
	var stale []models.ScenarioRecord
	if err := r.DB.Select("id, name, user_id").
		Where("pinned = ? AND last_used_at < ?", false, olderThan).
		Find(&stale).Error; err != nil {
		return 0, nil, fmt.Errorf("listing stale scenarios: %w", err)
	}
	if len(stale) == 0 {
		return 0, nil, nil
	}
	ids := make([]uint, len(stale))
	labels := make([]string, len(stale))
	for i, row := range stale {
		ids[i] = row.ID
		name := row.Name
		if name == "" {
			name = fmt.Sprintf("Scenario %d", row.ID)
		}
		labels[i] = fmt.Sprintf("%q (id=%d, user=%d)", name, row.ID, row.UserID)
	}
	result := r.DB.Where("id IN ?", ids).Delete(&models.ScenarioRecord{})
	if result.Error != nil {
		return 0, labels, fmt.Errorf("evicting stale scenarios: %w", result.Error)
	}
	return result.RowsAffected, labels, nil
}

// ParseSpec deserialises the SpecJSON field of a ScenarioRecord.
func ParseSpec(row *models.ScenarioRecord) (ScenarioSpec, error) {
	var spec ScenarioSpec
	if err := json.Unmarshal([]byte(row.SpecJSON), &spec); err != nil {
		return spec, fmt.Errorf("parsing scenario spec: %w", err)
	}
	return spec, nil
}
