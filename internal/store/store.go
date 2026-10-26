package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/jmoiron/sqlx"

	"github.com/ArtemMartirosian/secunda-task-service/internal/model"
)

var (
	ErrNotFound   = errors.New("not found")
	ErrForbidden  = errors.New("forbidden")
	ErrConflict   = errors.New("conflict")
	ErrValidation = errors.New("validation failed")
)

type Store struct {
	DB    *sqlx.DB
	Cache *redis.Client
}

func New(db *sqlx.DB, cache *redis.Client) *Store { return &Store{DB: db, Cache: cache} }

func (s *Store) CreateUser(ctx context.Context, email, hash, name string) (model.User, error) {
	result, err := s.DB.ExecContext(ctx, `INSERT INTO users(email,password_hash,name) VALUES(?,?,?)`, strings.ToLower(strings.TrimSpace(email)), hash, strings.TrimSpace(name))
	if err != nil {
		return model.User{}, err
	}
	id, _ := result.LastInsertId()
	return s.UserByID(ctx, id)
}

func (s *Store) UserByEmail(ctx context.Context, email string) (model.User, error) {
	var user model.User
	err := s.DB.GetContext(ctx, &user, `SELECT * FROM users WHERE email=?`, strings.ToLower(strings.TrimSpace(email)))
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return user, err
}

func (s *Store) UserByID(ctx context.Context, id int64) (model.User, error) {
	var user model.User
	err := s.DB.GetContext(ctx, &user, `SELECT * FROM users WHERE id=?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return user, err
}

func (s *Store) CreateTeam(ctx context.Context, userID int64, name string) (model.Team, error) {
	tx, err := s.DB.BeginTxx(ctx, nil)
	if err != nil {
		return model.Team{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO teams(name,created_by) VALUES(?,?)`, strings.TrimSpace(name), userID)
	if err != nil {
		return model.Team{}, err
	}
	id, _ := result.LastInsertId()
	if _, err = tx.ExecContext(ctx, `INSERT INTO team_members(team_id,user_id,role) VALUES(?,?,?)`, id, userID, model.RoleOwner); err != nil {
		return model.Team{}, err
	}
	if err = tx.Commit(); err != nil {
		return model.Team{}, err
	}
	return model.Team{ID: id, Name: strings.TrimSpace(name), CreatedBy: userID, Role: model.RoleOwner, CreatedAt: time.Now()}, nil
}

func (s *Store) Teams(ctx context.Context, userID int64) ([]model.Team, error) {
	var teams []model.Team
	err := s.DB.SelectContext(ctx, &teams, `SELECT t.*,tm.role FROM teams t JOIN team_members tm ON tm.team_id=t.id WHERE tm.user_id=? ORDER BY t.id DESC`, userID)
	return teams, err
}

func (s *Store) Role(ctx context.Context, teamID, userID int64) (string, error) {
	var role string
	err := s.DB.GetContext(ctx, &role, `SELECT role FROM team_members WHERE team_id=? AND user_id=?`, teamID, userID)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrForbidden
	}
	return role, err
}

func (s *Store) IsMember(ctx context.Context, teamID, userID int64) bool {
	_, err := s.Role(ctx, teamID, userID)
	return err == nil
}

func (s *Store) Invite(ctx context.Context, actorID, teamID, userID int64, role string) error {
	actorRole, err := s.Role(ctx, teamID, actorID)
	if err != nil || (actorRole != model.RoleOwner && actorRole != model.RoleAdmin) {
		return ErrForbidden
	}
	if role == model.RoleOwner || !model.ValidRoles[role] {
		return ErrValidation
	}
	var current string
	err = s.DB.GetContext(ctx, &current, `SELECT role FROM team_members WHERE team_id=? AND user_id=?`, teamID, userID)
	if err == nil && current == model.RoleOwner {
		return ErrForbidden
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO team_members(team_id,user_id,role) VALUES(?,?,?) ON DUPLICATE KEY UPDATE role=VALUES(role)`, teamID, userID, role)
	return err
}

func (s *Store) CreateTask(ctx context.Context, actorID int64, task model.Task) (model.Task, error) {
	if !s.IsMember(ctx, task.TeamID, actorID) {
		return model.Task{}, ErrForbidden
	}
	if task.AssigneeID != nil && !s.IsMember(ctx, task.TeamID, *task.AssigneeID) {
		return model.Task{}, ErrValidation
	}
	if task.Status == "" {
		task.Status = "todo"
	}
	if !validStatus(task.Status) || strings.TrimSpace(task.Title) == "" {
		return model.Task{}, ErrValidation
	}
	tx, err := s.DB.BeginTxx(ctx, nil)
	if err != nil {
		return model.Task{}, err
	}
	defer tx.Rollback()
	var closedAt *time.Time
	if task.Status == "done" {
		now := time.Now()
		closedAt = &now
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO tasks(team_id,title,description,status,created_by,assignee_id,closed_at) VALUES(?,?,?,?,?,?,?)`, task.TeamID, strings.TrimSpace(task.Title), task.Description, task.Status, actorID, task.AssigneeID, closedAt)
	if err != nil {
		return model.Task{}, err
	}
	id, _ := result.LastInsertId()
	changes, _ := json.Marshal(map[string]any{"created": true, "title": task.Title, "status": task.Status, "assignee_id": task.AssigneeID})
	if _, err = tx.ExecContext(ctx, `INSERT INTO task_history(task_id,changed_by,changes) VALUES(?,?,?)`, id, actorID, changes); err != nil {
		return model.Task{}, err
	}
	if err = tx.Commit(); err != nil {
		return model.Task{}, err
	}
	s.invalidateTasks(ctx, task.TeamID)
	return s.TaskByID(ctx, actorID, id)
}

func (s *Store) TaskByID(ctx context.Context, actorID, id int64) (model.Task, error) {
	var task model.Task
	err := s.DB.GetContext(ctx, &task, `SELECT * FROM tasks WHERE id=?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Task{}, ErrNotFound
	}
	if err != nil {
		return model.Task{}, err
	}
	if !s.IsMember(ctx, task.TeamID, actorID) {
		return model.Task{}, ErrForbidden
	}
	return task, nil
}

func (s *Store) ListTasks(ctx context.Context, actorID int64, filter model.TaskFilter) ([]model.Task, error) {
	if !s.IsMember(ctx, filter.TeamID, actorID) {
		return nil, ErrForbidden
	}
	if filter.Limit <= 0 || filter.Limit > 100 {
		filter.Limit = 20
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	version, _ := s.Cache.Get(ctx, fmt.Sprintf("tasks-version:%d", filter.TeamID)).Result()
	key := fmt.Sprintf("tasks:%d:%s:%v:%d:%d:%s", filter.TeamID, filter.Status, filter.AssigneeID, filter.Limit, filter.Offset, version)
	if raw, err := s.Cache.Get(ctx, key).Bytes(); err == nil {
		var tasks []model.Task
		if json.Unmarshal(raw, &tasks) == nil {
			return tasks, nil
		}
	}
	query := `SELECT * FROM tasks WHERE team_id=?`
	args := []any{filter.TeamID}
	if filter.Status != "" {
		query += ` AND status=?`
		args = append(args, filter.Status)
	}
	if filter.AssigneeID != nil {
		query += ` AND assignee_id=?`
		args = append(args, *filter.AssigneeID)
	}
	query += ` ORDER BY id DESC LIMIT ? OFFSET ?`
	args = append(args, filter.Limit, filter.Offset)
	var tasks []model.Task
	if err := s.DB.SelectContext(ctx, &tasks, query, args...); err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(tasks)
	_ = s.Cache.Set(ctx, key, raw, 5*time.Minute).Err()
	return tasks, nil
}

func validStatus(value string) bool {
	return value == "todo" || value == "in_progress" || value == "done"
}

func (s *Store) invalidateTasks(ctx context.Context, teamID int64) {
	_ = s.Cache.Incr(ctx, fmt.Sprintf("tasks-version:%d", teamID)).Err()
}

type TaskUpdate struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Status      *string `json:"status"`
	AssigneeID  *int64  `json:"assignee_id"`
	Version     int64   `json:"version"`
}

func (s *Store) UpdateTask(ctx context.Context, actorID, taskID int64, update TaskUpdate) (model.Task, error) {
	tx, err := s.DB.BeginTxx(ctx, nil)
	if err != nil {
		return model.Task{}, err
	}
	defer tx.Rollback()
	var current model.Task
	if err = tx.GetContext(ctx, &current, `SELECT * FROM tasks WHERE id=? FOR UPDATE`, taskID); errors.Is(err, sql.ErrNoRows) {
		return model.Task{}, ErrNotFound
	} else if err != nil {
		return model.Task{}, err
	}
	var role string
	if err = tx.GetContext(ctx, &role, `SELECT role FROM team_members WHERE team_id=? AND user_id=?`, current.TeamID, actorID); err != nil {
		return model.Task{}, ErrForbidden
	}
	if update.Version == 0 || update.Version != current.Version {
		return model.Task{}, ErrConflict
	}
	isManager := role == model.RoleOwner || role == model.RoleAdmin
	isCreator := actorID == current.CreatedBy
	isAssignee := current.AssigneeID != nil && actorID == *current.AssigneeID
	if !isManager && !isCreator && !isAssignee {
		return model.Task{}, ErrForbidden
	}
	if isAssignee && !isCreator && !isManager && (update.Title != nil || update.Description != nil || update.AssigneeID != nil) {
		return model.Task{}, ErrForbidden
	}
	if update.AssigneeID != nil {
		var exists int
		if err = tx.GetContext(ctx, &exists, `SELECT 1 FROM team_members WHERE team_id=? AND user_id=?`, current.TeamID, *update.AssigneeID); err != nil {
			return model.Task{}, ErrValidation
		}
	}
	next := current
	if update.Title != nil {
		next.Title = strings.TrimSpace(*update.Title)
	}
	if update.Description != nil {
		next.Description = *update.Description
	}
	if update.Status != nil {
		next.Status = *update.Status
	}
	if update.AssigneeID != nil {
		next.AssigneeID = update.AssigneeID
	}
	if next.Title == "" || !validStatus(next.Status) {
		return model.Task{}, ErrValidation
	}
	if next.Status == "done" && current.Status != "done" {
		now := time.Now()
		next.ClosedAt = &now
	}
	if next.Status != "done" {
		next.ClosedAt = nil
	}
	changes := map[string]any{}
	if next.Title != current.Title {
		changes["title"] = map[string]any{"old": current.Title, "new": next.Title}
	}
	if next.Description != current.Description {
		changes["description"] = map[string]any{"old": current.Description, "new": next.Description}
	}
	if next.Status != current.Status {
		changes["status"] = map[string]any{"old": current.Status, "new": next.Status}
	}
	if !sameInt64Ptr(next.AssigneeID, current.AssigneeID) {
		changes["assignee_id"] = map[string]any{"old": current.AssigneeID, "new": next.AssigneeID}
	}
	if len(changes) == 0 {
		return current, tx.Commit()
	}
	result, err := tx.ExecContext(ctx, `UPDATE tasks SET title=?,description=?,status=?,assignee_id=?,closed_at=?,version=version+1 WHERE id=? AND version=?`, next.Title, next.Description, next.Status, next.AssigneeID, next.ClosedAt, current.ID, current.Version)
	if err != nil {
		return model.Task{}, err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return model.Task{}, ErrConflict
	}
	raw, _ := json.Marshal(changes)
	if _, err = tx.ExecContext(ctx, `INSERT INTO task_history(task_id,changed_by,changes) VALUES(?,?,?)`, current.ID, actorID, raw); err != nil {
		return model.Task{}, err
	}
	if err = tx.Commit(); err != nil {
		return model.Task{}, err
	}
	s.invalidateTasks(ctx, current.TeamID)
	return s.TaskByID(ctx, actorID, current.ID)
}

func sameInt64Ptr(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func (s *Store) History(ctx context.Context, actorID, taskID int64) ([]model.TaskHistory, error) {
	if _, err := s.TaskByID(ctx, actorID, taskID); err != nil {
		return nil, err
	}
	var history []model.TaskHistory
	err := s.DB.SelectContext(ctx, &history, `SELECT * FROM task_history WHERE task_id=? ORDER BY id`, taskID)
	return history, err
}

func (s *Store) AddComment(ctx context.Context, actorID, taskID int64, content string) (model.Comment, error) {
	if _, err := s.TaskByID(ctx, actorID, taskID); err != nil {
		return model.Comment{}, err
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return model.Comment{}, ErrValidation
	}
	result, err := s.DB.ExecContext(ctx, `INSERT INTO task_comments(task_id,user_id,content) VALUES(?,?,?)`, taskID, actorID, content)
	if err != nil {
		return model.Comment{}, err
	}
	id, _ := result.LastInsertId()
	var comment model.Comment
	err = s.DB.GetContext(ctx, &comment, `SELECT * FROM task_comments WHERE id=?`, id)
	return comment, err
}

func (s *Store) Comments(ctx context.Context, actorID, taskID int64) ([]model.Comment, error) {
	if _, err := s.TaskByID(ctx, actorID, taskID); err != nil {
		return nil, err
	}
	var comments []model.Comment
	err := s.DB.SelectContext(ctx, &comments, `SELECT * FROM task_comments WHERE task_id=? ORDER BY id`, taskID)
	return comments, err
}

type statsRow struct {
	Kind     string   `db:"kind"`
	Label    *string  `db:"label"`
	UserID   *int64   `db:"user_id"`
	UserName *string  `db:"user_name"`
	Value    *float64 `db:"value"`
}

func (s *Store) TeamStats(ctx context.Context, actorID, teamID int64) (model.TeamStats, error) {
	role, err := s.Role(ctx, teamID, actorID)
	if err != nil || (role != model.RoleOwner && role != model.RoleAdmin) {
		return model.TeamStats{}, ErrForbidden
	}
	const query = `
WITH status_stats AS (
  SELECT status AS label, COUNT(*) AS value FROM tasks WHERE team_id=? GROUP BY status
), top_assignees AS (
  SELECT u.id AS user_id, u.name AS user_name, COUNT(*) AS value
  FROM tasks t JOIN users u ON u.id=t.assignee_id
  WHERE t.team_id=? AND t.status='done' AND t.closed_at >= NOW() - INTERVAL 30 DAY
  GROUP BY u.id,u.name ORDER BY value DESC LIMIT 3
), close_stats AS (
  SELECT AVG(TIMESTAMPDIFF(SECOND,created_at,closed_at)) AS value FROM tasks WHERE team_id=? AND closed_at IS NOT NULL
), comment_stats AS (
  SELECT COUNT(c.id) AS value FROM task_comments c JOIN tasks t ON t.id=c.task_id WHERE t.team_id=?
)
SELECT 'status' AS kind,label,NULL AS user_id,NULL AS user_name,value FROM status_stats
UNION ALL SELECT 'top',NULL,user_id,user_name,value FROM top_assignees
UNION ALL SELECT 'average',NULL,NULL,NULL,value FROM close_stats
UNION ALL SELECT 'comments',NULL,NULL,NULL,value FROM comment_stats`
	var rows []statsRow
	if err = s.DB.SelectContext(ctx, &rows, query, teamID, teamID, teamID, teamID); err != nil {
		return model.TeamStats{}, err
	}
	stats := model.TeamStats{TasksByStatus: map[string]int64{}, TopAssignees: []model.AssigneeStat{}}
	for _, row := range rows {
		switch row.Kind {
		case "status":
			if row.Label != nil && row.Value != nil {
				stats.TasksByStatus[*row.Label] = int64(*row.Value)
			}
		case "top":
			if row.UserID != nil && row.UserName != nil && row.Value != nil {
				stats.TopAssignees = append(stats.TopAssignees, model.AssigneeStat{UserID: *row.UserID, Name: *row.UserName, ClosedTasks: int64(*row.Value)})
			}
		case "average":
			stats.AverageCloseSec = row.Value
		case "comments":
			if row.Value != nil {
				stats.CommentCount = int64(*row.Value)
			}
		}
	}
	return stats, nil
}
