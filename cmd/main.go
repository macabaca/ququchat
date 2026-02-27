package main

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"ququchat/internal/api"
	"ququchat/internal/config"
	database "ququchat/internal/server/db"
	"ququchat/internal/server/storage"
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

	r := api.SetupRouter(db, authCfg, cfg.Chat, cfg.File, objStorage, bucket)

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
