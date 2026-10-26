//go:build integration

package store

import (
	"context"
	"fmt"
	"math"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
)

func TestTeamStatsIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN is not set")
	}
	db, err := sqlx.Connect("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	raw, err := os.ReadFile("../../cmd/api/migrations/001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(raw)); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	email := fmt.Sprintf("stats-%d@example.com", time.Now().UnixNano())
	result, err := db.ExecContext(ctx, `INSERT INTO users(email,password_hash,name) VALUES(?, 'hash', 'Owner')`, email)
	if err != nil {
		t.Fatal(err)
	}
	ownerID, _ := result.LastInsertId()
	result, err = db.ExecContext(ctx, `INSERT INTO teams(name,created_by) VALUES('Stats team',?)`, ownerID)
	if err != nil {
		t.Fatal(err)
	}
	teamID, _ := result.LastInsertId()
	if _, err = db.ExecContext(ctx, `INSERT INTO team_members(team_id,user_id,role) VALUES(?,?,'owner')`, teamID, ownerID); err != nil {
		t.Fatal(err)
	}
	result, err = db.ExecContext(ctx, `INSERT INTO tasks(team_id,title,description,status,created_by,assignee_id,created_at,closed_at) VALUES(?, 'Done', '', 'done', ?, ?, NOW()-INTERVAL 2 HOUR, NOW())`, teamID, ownerID, ownerID)
	if err != nil {
		t.Fatal(err)
	}
	taskID, _ := result.LastInsertId()
	if _, err = db.ExecContext(ctx, `INSERT INTO tasks(team_id,title,description,status,created_by) VALUES(?, 'Todo', '', 'todo', ?)`, teamID, ownerID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO task_comments(task_id,user_id,content) VALUES(?,?, 'comment')`, taskID, ownerID); err != nil {
		t.Fatal(err)
	}
	s := New(db, nil)
	stats, err := s.TeamStats(ctx, ownerID, teamID)
	if err != nil {
		t.Fatal(err)
	}
	if stats.TasksByStatus["done"] != 1 || stats.TasksByStatus["todo"] != 1 {
		t.Fatalf("unexpected status stats: %#v", stats.TasksByStatus)
	}
	if len(stats.TopAssignees) != 1 || stats.TopAssignees[0].ClosedTasks != 1 {
		t.Fatalf("unexpected top assignees: %#v", stats.TopAssignees)
	}
	if stats.AverageCloseSec == nil || math.Abs(*stats.AverageCloseSec-7200) > 10 {
		t.Fatalf("unexpected average: %#v", stats.AverageCloseSec)
	}
	if stats.CommentCount != 1 {
		t.Fatalf("unexpected comment count: %d", stats.CommentCount)
	}
}
