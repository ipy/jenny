package tool

import (
	"fmt"
	"sync"
	"time"
)

// TaskStatus represents the status of a task.
type TaskStatus string

const (
	TaskStatusPending    TaskStatus = "pending"
	TaskStatusInProgress TaskStatus = "in_progress"
	TaskStatusCompleted  TaskStatus = "completed"
	TaskStatusDeleted    TaskStatus = "deleted"
)

// Task represents a tracked task record.
type Task struct {
	ID          string
	Subject     string
	Description string
	ActiveForm  string
	Status      TaskStatus
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Metadata    map[string]any
}

// TaskFilter contains optional filters for listing tasks.
type TaskFilter struct {
	Status TaskStatus
}

// TaskStore is a thread-safe in-memory store for tasks.
type TaskStore struct {
	mu    sync.RWMutex
	tasks map[string]*Task
}

// NewTaskStore creates a new TaskStore.
func NewTaskStore() *TaskStore {
	return &TaskStore{
		tasks: make(map[string]*Task),
	}
}

// generateID generates a unique 16-character hex ID using crypto/rand.
func generateID() (string, error) {
	b := make([]byte, 8)
	_, err := randRead(b)
	if err != nil {
		return "", fmt.Errorf("generating ID: %w", err)
	}
	return fmt.Sprintf("%x", b), nil
}

// Create adds a new task and returns it. The ID is generated and CreatedAt is set.
func (s *TaskStore) Create(subject, description, activeForm string, metadata map[string]any) (*Task, error) {
	id, err := generateID()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	task := &Task{
		ID:          id,
		Subject:     subject,
		Description: description,
		ActiveForm:  activeForm,
		Status:      TaskStatusPending,
		CreatedAt:   now,
		UpdatedAt:   now,
		Metadata:    metadata,
	}

	s.mu.Lock()
	s.tasks[id] = task
	s.mu.Unlock()

	return task, nil
}

// Get retrieves a task by ID. Returns nil if not found.
func (s *TaskStore) Get(id string) *Task {
	s.mu.RLock()
	task := s.tasks[id]
	s.mu.RUnlock()
	return task
}

// List returns all tasks, optionally filtered.
func (s *TaskStore) List(filter TaskFilter) []*Task {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*Task
	for _, task := range s.tasks {
		if filter.Status != "" && task.Status != filter.Status {
			continue
		}
		result = append(result, task)
	}
	return result
}

// Update updates specified fields on a task. Returns the updated task or nil if not found.
func (s *TaskStore) Update(id string, fields map[string]any) *Task {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[id]
	if !ok {
		return nil
	}

	if subject, ok := fields["subject"].(string); ok {
		task.Subject = subject
	}
	if description, ok := fields["description"].(string); ok {
		task.Description = description
	}
	if activeForm, ok := fields["active_form"].(string); ok {
		task.ActiveForm = activeForm
	}
	if status, ok := fields["status"].(string); ok {
		task.Status = TaskStatus(status)
	}
	if metadata, ok := fields["metadata"].(map[string]any); ok {
		task.Metadata = metadata
	}
	task.UpdatedAt = time.Now()

	return task
}

// Delete removes a task by ID.
func (s *TaskStore) Delete(id string) {
	s.mu.Lock()
	delete(s.tasks, id)
	s.mu.Unlock()
}
