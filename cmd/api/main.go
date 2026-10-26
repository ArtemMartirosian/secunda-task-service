package main

import (
	"context"
	"embed"
	"log"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"

	"github.com/ArtemMartirosian/secunda-task-service/internal/config"
	"github.com/ArtemMartirosian/secunda-task-service/internal/httpapi"
	"github.com/ArtemMartirosian/secunda-task-service/internal/store"
)

//go:embed migrations/*.sql
var migrations embed.FS

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	db := connectMySQL(cfg.MySQLDSN)
	cache := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr, Password: cfg.RedisPass, DB: cfg.RedisDB})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = cache.Ping(ctx).Err(); err != nil {
		log.Fatal(err)
	}
	raw, err := migrations.ReadFile("migrations/001_init.sql")
	if err != nil {
		log.Fatal(err)
	}
	if _, err = db.Exec(string(raw)); err != nil {
		log.Fatal(err)
	}
	api := &httpapi.API{Store: store.New(db, cache), JWTSecret: cfg.JWTSecret, JWTDuration: cfg.JWTDuration}
	log.Printf("listening on %s", cfg.HTTPAddr)
	log.Fatal(api.Router().Run(cfg.HTTPAddr))
}

func connectMySQL(dsn string) *sqlx.DB {
	var db *sqlx.DB
	var err error
	for attempt := 0; attempt < 30; attempt++ {
		db, err = sqlx.Connect("mysql", dsn)
		if err == nil {
			db.SetMaxOpenConns(20)
			db.SetConnMaxLifetime(3 * time.Minute)
			return db
		}
		if !strings.Contains(err.Error(), "connection refused") && !strings.Contains(err.Error(), "Unknown MySQL server host") {
			log.Printf("waiting for mysql: %v", err)
		}
		time.Sleep(2 * time.Second)
	}
	log.Fatal(err)
	return nil
}
