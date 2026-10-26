package model

import "time"

const (
	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleMember = "member"
)

var ValidRoles = map[string]bool{RoleOwner: true, RoleAdmin: true, RoleMember: true}

type User struct {
	ID           int64     `db:"id" json:"id"`
	Email        string    `db:"email" json:"email"`
	PasswordHash string    `db:"password_hash" json:"-"`
	Name         string    `db:"name" json:"name"`
	CreatedAt    time.Time `db:"created_at" json:"created_at"`
}

type Team struct {
	ID        int64     `db:"id" json:"id"`
	Name      string    `db:"name" json:"name"`
	CreatedBy int64     `db:"created_by" json:"created_by"`
	Role      string    `db:"role" json:"role,omitempty"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

type Task struct {
	ID          int64      `db:"id" json:"id"`
	TeamID      int64      `db:"team_id" json:"team_id"`
	Title       string     `db:"title" json:"title"`
	Description string     `db:"description" json:"description"`
	Status      string     `db:"status" json:"status"`
	CreatedBy   int64      `db:"created_by" json:"created_by"`
	AssigneeID  *int64     `db:"assignee_id" json:"assignee_id,omitempty"`
	CreatedAt   time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt   time.Time  `db:"updated_at" json:"updated_at"`
	ClosedAt    *time.Time `db:"closed_at" json:"closed_at,omitempty"`
	Version     int64      `db:"version" json:"version"`
}

type TaskHistory struct {
	ID        int64     `db:"id" json:"id"`
	TaskID    int64     `db:"task_id" json:"task_id"`
	ChangedBy int64     `db:"changed_by" json:"changed_by"`
	Changes   string    `db:"changes" json:"changes"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

type Comment struct {
	ID        int64     `db:"id" json:"id"`
	TaskID    int64     `db:"task_id" json:"task_id"`
	UserID    int64     `db:"user_id" json:"user_id"`
	Content   string    `db:"content" json:"content"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

type TaskFilter struct {
	TeamID     int64
	Status     string
	AssigneeID *int64
	Limit      int
	Offset     int
}

type TeamStats struct {
	TasksByStatus   map[string]int64 `json:"tasks_by_status"`
	TopAssignees    []AssigneeStat   `json:"top_assignees"`
	AverageCloseSec *float64         `json:"average_close_seconds"`
	CommentCount    int64            `json:"comment_count"`
}

type AssigneeStat struct {
	UserID      int64  `json:"user_id"`
	Name        string `json:"name"`
	ClosedTasks int64  `json:"closed_tasks"`
}
