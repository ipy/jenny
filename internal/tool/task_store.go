package tool

import (
	"encoding/json"
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
	ID          string         `json:"id"`
	Subject     string         `json:"subject"`
	Description string         `json:"description"`
	ActiveForm  string         `json:"active_form"`
	Status      TaskStatus     `json:"status"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	Blocks      []string       `json:"blocks,omitempty"`
	BlockedBy   []string       `json:"blocked_by,omitempty"`
}

// TaskFilter contains optional filters for listing tasks.
type TaskFilter struct {
	Status TaskStatus
}

// TaskStore is a thread-safe in-memory store for tasks.
type TaskStore struct {
	mu       sync.RWMutex
	tasks    map[string]*Task
	onChange func() // Called after any mutation; used for persistence
}

// NewTaskStore creates a new TaskStore.
func NewTaskStore() *TaskStore {
	return &TaskStore{
		tasks: make(map[string]*Task),
	}
}

// SetOnChange sets a callback invoked after every mutation (Create, Update, AddDependencies, Delete).
func (s *TaskStore) SetOnChange(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onChange = fn
}

func (s *TaskStore) fireChange() {
	if s.onChange != nil {
		s.onChange()
	}
}

// AllTasks returns a snapshot of all tasks for serialization.
func (s *TaskStore) AllTasks() []*Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		result = append(result, t)
	}
	return result
}

// LoadFromJSON replaces the store contents from a JSON representation.
// The incoming tasks are deep-copied so the caller retains ownership of the slice.
func (s *TaskStore) LoadFromJSON(data []byte) error {
	var tasks []*Task
	if err := json.Unmarshal(data, &tasks); err != nil {
		return fmt.Errorf("unmarshaling tasks: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks = make(map[string]*Task, len(tasks))
	for _, t := range tasks {
		if t.ID == "" {
			continue
		}
		s.tasks[t.ID] = t
	}
	return nil
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
	s.fireChange()

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
	s.fireChange()

	return task
}

// AddDependencies adds blocks/blockedBy relationships to a task.
func (s *TaskStore) AddDependencies(id string, blocks, blockedBy []string) *Task {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[id]
	if !ok {
		return nil
	}

	if len(blocks) > 0 {
		task.Blocks = append(task.Blocks, blocks...)
	}
	if len(blockedBy) > 0 {
		task.BlockedBy = append(task.BlockedBy, blockedBy...)
	}
	task.UpdatedAt = time.Now()
	s.fireChange()

	return task
}

// Delete removes a task by ID.
func (s *TaskStore) Delete(id string) {
	s.mu.Lock()
	delete(s.tasks, id)
	s.mu.Unlock()
	s.fireChange()
}
