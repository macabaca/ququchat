package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"ququchat/internal/api"
	"ququchat/internal/config"
	cachepkg "ququchat/internal/server/cache"
	database "ququchat/internal/server/db"
	"ququchat/internal/server/storage"
	taskservice "ququchat/internal/service"
	tasksvc "ququchat/internal/service/task"
)

func main() {
	// 加载配置
	cfg, err := config.LoadDefault()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 初始化数据库连接（使用 server/db 包）
	db, err := database.OpenGorm(cfg.Database)
	if err != nil {
		log.Fatalf("数据库连接失败: %v", err)
	}
	// 基础检查底层连接可用
	if sqlDB, err := db.DB(); err != nil {
		log.Fatalf("获取底层连接失败: %v", err)
	} else if err := sqlDB.Ping(); err != nil {
		log.Fatalf("数据库不可用: %v", err)
	}

	// 自动迁移数据库结构
	log.Println("正在检查并迁移数据库结构...")
	if err := database.Migrate(db); err != nil {
		log.Fatalf("数据库迁移失败: %v", err)
	}
	log.Println("数据库迁移完成")

	affected, err := database.ResetAllUsersOffline(db)
	if err != nil {
		log.Fatalf("重置用户状态失败: %v", err)
	}
	log.Printf("用户状态重置完成，影响行数: %d", affected)

	provider := cfg.Storage.ProviderOrDefault()
	var objStorage storage.ObjectStorage
	var bucket string
	switch provider {
	case "minio":
		objStorage, err = storage.InitMinioStorage(cfg.Minio)
		if err != nil {
			log.Fatalf("MinIO 连接失败: %v", err)
		}
		bucket = cfg.Minio.Bucket
	case "oss":
		objStorage, err = storage.InitOSSStorage(cfg.OSS)
		if err != nil {
			log.Fatalf("OSS 连接失败: %v", err)
		}
		bucket = cfg.OSS.Bucket
	default:
		log.Fatalf("不支持的对象存储 provider: %s", provider)
	}

	authCfg := cfg.Auth.ToSettings()
	commandPriorityRules := make([]taskservice.CommandPriorityRule, 0)
	for _, item := range cfg.TaskPriority.NormalizedRules() {
		commandPriorityRules = append(commandPriorityRules, taskservice.CommandPriorityRule{
			Prefix:   item.Task,
			Priority: tasksvc.Priority(item.Priority),
		})
	}
	taskService := taskservice.NewMainServiceWithOptions(db, tasksvc.RuntimeOptions{
		QueueTransport:                   cfg.Task.QueueTransportOrDefault(),
		QueueRabbitMQURL:                 cfg.Task.QueueRabbitMQURL,
		QueueRabbitMQName:                cfg.Task.QueueRabbitMQNameOrDefault(),
		QueueRabbitMQExchange:            cfg.Task.QueueRabbitMQExchangeOrDefault(),
		QueueRabbitMQMaxPriority:         cfg.Task.QueueRabbitMQMaxPriorityOrDefault(),
		QueueRabbitMQMaxLength:           cfg.Task.QueueRabbitMQMaxLengthOrDefault(),
		DoneEventRabbitMQURL:             cfg.Task.DoneEventMQURLOrDefault(),
		DoneEventQueueName:               cfg.Task.DoneEventQueueOrDefault(),
		DoneEventQueueMaxLength:          cfg.Task.DoneEventQueueMaxLengthOrDefault(),
		DoneEventQueueMessageTTL:         cfg.Task.DoneEventQueueMessageTTLOrDefault(),
		DoneEventConsumeRetryMaxAttempts: cfg.Task.DoneConsumeRetryMaxAttemptsOrDefault(),
		DoneEventConsumeRetryDelay:       cfg.Task.DoneConsumeRetryDelayOrDefault(),
		InputRetryMaxAttempts:            cfg.Task.InputRetryMaxAttemptsOrDefault(),
		InputRetryDelay:                  cfg.Task.InputRetryDelayOrDefault(),
	}, taskservice.ServiceOptions{
		CommandPriorityRules: commandPriorityRules,
	})
	log.Printf("主进程不启动 Task Runtime，仅提供任务提交与状态能力")

	redisClient := cachepkg.NewRedisClient(cachepkg.RedisOptions{
		Addr:           cfg.Redis.Addr,
		Password:       cfg.Redis.Password,
		DB:             cfg.Redis.DB,
		KeyPrefix:      cfg.Redis.KeyPrefix,
		DialTimeoutMs:  cfg.Redis.DialTimeoutMs,
		ReadTimeoutMs:  cfg.Redis.ReadTimeoutMs,
		WriteTimeoutMs: cfg.Redis.WriteTimeoutMs,
	})
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 2*time.Second)
	if err := redisClient.Ping(pingCtx); err != nil {
		log.Printf("Redis 不可用，缓存已降级为 DB 直连: %v", err)
		_ = redisClient.Close()
		redisClient = nil
	}
	pingCancel()
	if redisClient != nil {
		defer func() {
			if err := redisClient.Close(); err != nil {
				log.Printf("Redis 关闭失败: %v", err)
			}
		}()
	}

	r := api.SetupRouter(db, authCfg, cfg.Chat, cfg.File, cfg.Avatar, objStorage, bucket, redisClient, taskService)

	// 简单首页/健康检查（便于开发验证）
	r.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true, "service": "ququchat"})
	})

	// 启动服务
	addr := ":8080"
	log.Printf("HTTP 服务器已启动: http://localhost%v", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("Gin 启动失败: %v", err)
	}
}
