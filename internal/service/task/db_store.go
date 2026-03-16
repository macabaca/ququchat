package tasksvc

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"ququchat/internal/models"
)

type GormStore struct {
	db *gorm.DB
}

func NewGormStore(db *gorm.DB) *GormStore {
	return &GormStore{db: db}
}

func (s *GormStore) Create(t *Task) error {
	if s == nil || s.db == nil {
		return errors.New("gorm store db is nil")
	}
	row, err := toTaskJob(t)
	if err != nil {
		return err
	}
	return s.db.Create(row).Error
}

func (s *GormStore) Get(taskID string) (*Task, bool) {
	if s == nil || s.db == nil {
		return nil, false
	}
	var row models.TaskJob
	err := s.db.Where("id = ?", strings.TrimSpace(taskID)).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false
	}
	if err != nil {
		return nil, false
	}
	t, convertErr := fromTaskJob(&row)
	if convertErr != nil {
		return nil, false
	}
	return t, true
}

func (s *GormStore) MarkRunning(taskID string) (*Task, error) {
	return s.updateStatus(strings.TrimSpace(taskID), func(t *Task) {
		t.Status = StatusRunning
		t.UpdatedAt = time.Now()
	})
}

func (s *GormStore) MarkSucceeded(taskID string, result Result) (*Task, error) {
	return s.updateStatus(strings.TrimSpace(taskID), func(t *Task) {
		t.Status = StatusSucceeded
		t.Result = result
		t.ErrorMessage = ""
		t.UpdatedAt = time.Now()
	})
}

func (s *GormStore) MarkFailed(taskID string, message string) (*Task, error) {
	return s.updateStatus(strings.TrimSpace(taskID), func(t *Task) {
		t.Status = StatusFailed
		t.ErrorMessage = message
		t.UpdatedAt = time.Now()
	})
}

func (s *GormStore) updateStatus(taskID string, mutate func(t *Task)) (*Task, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("gorm store db is nil")
	}
	var doneTask *Task
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var row models.TaskJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", taskID).First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrTaskNotFound
			}
			return err
		}
		t, err := fromTaskJob(&row)
		if err != nil {
			return err
		}
		mutate(t)
		nextRow, err := toTaskJob(t)
		if err != nil {
			return err
		}
		if err := tx.Model(&models.TaskJob{}).Where("id = ?", taskID).Updates(map[string]interface{}{
			"status":        nextRow.Status,
			"result_json":   nextRow.ResultJSON,
			"error_message": nextRow.ErrorMessage,
			"updated_at":    nextRow.UpdatedAt,
		}).Error; err != nil {
			return err
		}
		doneTask = t.Clone()
		return nil
	})
	if err != nil {
		return nil, err
	}
	return doneTask, nil
}

func toTaskJob(t *Task) (*models.TaskJob, error) {
	if t == nil {
		return nil, errors.New("task is nil")
	}
	payloadJSON, err := json.Marshal(t.Payload)
	if err != nil {
		return nil, err
	}
	resultJSON, err := json.Marshal(t.Result)
	if err != nil {
		return nil, err
	}
	return &models.TaskJob{
		ID:           t.ID,
		RequestID:    t.RequestID,
		TaskType:     string(t.Type),
		Priority:     int(t.Priority),
		Status:       string(t.Status),
		PayloadJSON:  datatypes.JSON(payloadJSON),
		ResultJSON:   datatypes.JSON(resultJSON),
		ErrorMessage: t.ErrorMessage,
		CreatedAt:    t.CreatedAt,
		UpdatedAt:    t.UpdatedAt,
	}, nil
}

func fromTaskJob(row *models.TaskJob) (*Task, error) {
	if row == nil {
		return nil, errors.New("task row is nil")
	}
	var payload Payload
	if len(row.PayloadJSON) > 0 {
		if err := json.Unmarshal(row.PayloadJSON, &payload); err != nil {
			return nil, err
		}
	}
	var result Result
	if len(row.ResultJSON) > 0 {
		if err := json.Unmarshal(row.ResultJSON, &result); err != nil {
			return nil, err
		}
	}
	return (&Task{
		ID:           row.ID,
		RequestID:    row.RequestID,
		Type:         Type(row.TaskType),
		Priority:     Priority(row.Priority),
		Status:       Status(row.Status),
		Payload:      payload,
		Result:       result,
		ErrorMessage: row.ErrorMessage,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}).Clone(), nil
}
