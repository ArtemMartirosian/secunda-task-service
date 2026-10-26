package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ArtemMartirosian/secunda-task-service/internal/auth"
	"github.com/ArtemMartirosian/secunda-task-service/internal/model"
	"github.com/ArtemMartirosian/secunda-task-service/internal/store"
)

const userKey = "user_id"

type API struct {
	Store       *store.Store
	JWTSecret   string
	JWTDuration time.Duration
}

func (a *API) Router() *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery(), gin.Logger())
	r.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	r.StaticFile("/openapi.yaml", "docs/openapi.yaml")
	v1 := r.Group("/api/v1")
	v1.POST("/register", a.register)
	v1.POST("/login", a.login)
	authed := v1.Group("")
	authed.Use(a.authenticate)
	authed.POST("/teams", a.createTeam)
	authed.GET("/teams", a.teams)
	authed.POST("/teams/:id/invite", a.invite)
	authed.GET("/teams/:id/stats", a.stats)
	authed.POST("/tasks", a.createTask)
	authed.GET("/tasks", a.tasks)
	authed.PUT("/tasks/:id", a.updateTask)
	authed.GET("/tasks/:id/history", a.history)
	authed.POST("/tasks/:id/comments", a.addComment)
	authed.GET("/tasks/:id/comments", a.comments)
	return r
}

func (a *API) register(c *gin.Context) {
	var body struct{ Email, Password, Name string }
	if c.ShouldBindJSON(&body) != nil || !strings.Contains(body.Email, "@") || strings.TrimSpace(body.Name) == "" {
		fail(c, store.ErrValidation)
		return
	}
	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		fail(c, store.ErrValidation)
		return
	}
	user, err := a.Store.CreateUser(c, body.Email, hash, body.Name)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "email already registered"})
		return
	}
	c.JSON(http.StatusCreated, user)
}

func (a *API) login(c *gin.Context) {
	var body struct{ Email, Password string }
	if c.ShouldBindJSON(&body) != nil {
		fail(c, store.ErrValidation)
		return
	}
	user, err := a.Store.UserByEmail(c, body.Email)
	if err != nil || !auth.CheckPassword(user.PasswordHash, body.Password) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}
	token, err := auth.Issue(a.JWTSecret, a.JWTDuration, user.ID)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token, "user": user})
}

func (a *API) authenticate(c *gin.Context) {
	header := c.GetHeader("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "bearer token required"})
		return
	}
	userID, err := auth.Parse(a.JWTSecret, strings.TrimPrefix(header, "Bearer "))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}
	c.Set(userKey, userID)
	c.Next()
}

func (a *API) createTeam(c *gin.Context) {
	var body struct {
		Name string `json:"name"`
	}
	if c.ShouldBindJSON(&body) != nil || strings.TrimSpace(body.Name) == "" {
		fail(c, store.ErrValidation)
		return
	}
	team, err := a.Store.CreateTeam(c, userID(c), body.Name)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, team)
}

func (a *API) teams(c *gin.Context) {
	teams, err := a.Store.Teams(c, userID(c))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, teams)
}

func (a *API) invite(c *gin.Context) {
	teamID, ok := paramID(c)
	if !ok {
		return
	}
	var body struct {
		UserID int64  `json:"user_id"`
		Role   string `json:"role"`
	}
	if c.ShouldBindJSON(&body) != nil || body.UserID == 0 {
		fail(c, store.ErrValidation)
		return
	}
	if err := a.Store.Invite(c, userID(c), teamID, body.UserID, body.Role); err != nil {
		fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (a *API) createTask(c *gin.Context) {
	var task model.Task
	if c.ShouldBindJSON(&task) != nil {
		fail(c, store.ErrValidation)
		return
	}
	task, err := a.Store.CreateTask(c, userID(c), task)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, task)
}

func (a *API) tasks(c *gin.Context) {
	teamID, err := strconv.ParseInt(c.Query("team_id"), 10, 64)
	if err != nil || teamID == 0 {
		fail(c, store.ErrValidation)
		return
	}
	filter := model.TaskFilter{TeamID: teamID, Status: c.Query("status"), Limit: intQuery(c, "limit", 20), Offset: intQuery(c, "offset", 0)}
	if raw := c.Query("assignee_id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			fail(c, store.ErrValidation)
			return
		}
		filter.AssigneeID = &id
	}
	tasks, err := a.Store.ListTasks(c, userID(c), filter)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, tasks)
}

func (a *API) updateTask(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	var update store.TaskUpdate
	if c.ShouldBindJSON(&update) != nil {
		fail(c, store.ErrValidation)
		return
	}
	task, err := a.Store.UpdateTask(c, userID(c), id, update)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, task)
}

func (a *API) history(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	history, err := a.Store.History(c, userID(c), id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, history)
}

func (a *API) addComment(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if c.ShouldBindJSON(&body) != nil {
		fail(c, store.ErrValidation)
		return
	}
	comment, err := a.Store.AddComment(c, userID(c), id, body.Content)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, comment)
}

func (a *API) comments(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	comments, err := a.Store.Comments(c, userID(c), id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, comments)
}

func (a *API) stats(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	stats, err := a.Store.TeamStats(c, userID(c), id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, stats)
}

func userID(c *gin.Context) int64 { value, _ := c.Get(userKey); return value.(int64) }

func paramID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		fail(c, store.ErrValidation)
		return 0, false
	}
	return id, true
}

func intQuery(c *gin.Context, key string, fallback int) int {
	value, err := strconv.Atoi(c.Query(key))
	if err != nil {
		return fallback
	}
	return value
}

func fail(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	message := "internal error"
	switch {
	case errors.Is(err, store.ErrValidation):
		status = http.StatusBadRequest
		message = err.Error()
	case errors.Is(err, store.ErrForbidden):
		status = http.StatusForbidden
		message = err.Error()
	case errors.Is(err, store.ErrNotFound):
		status = http.StatusNotFound
		message = err.Error()
	case errors.Is(err, store.ErrConflict):
		status = http.StatusConflict
		message = "task was changed; reload and retry"
	}
	c.JSON(status, gin.H{"error": message})
}
