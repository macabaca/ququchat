package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/url"

	_ "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"ququchat/internal/config"
	"ququchat/internal/models"
)

func main() {
	cfg, err := config.LoadDefault()
	if err != nil {
		log.Fatalf("读取配置失败: %v", err)
	}
	if cfg.Database.Driver != "mysql" {
		log.Fatalf("当前迁移程序仅支持 mysql，配置为: %s", cfg.Database.Driver)
	}
	if cfg.Database.User == "" {
		log.Fatalf("数据库用户未配置")
	}
	if cfg.Database.Password == "" {
		log.Println("提示: 配置中的 password 为空，请在 internal/config/config.yaml 填写你的密码")
	}

	// 1) 确保数据库存在
	serverDSN := buildDSN(cfg.Database, "")
	dbServer, err := sql.Open("mysql", serverDSN)
	if err != nil {
		log.Fatalf("连接到 MySQL 服务器失败: %v", err)
	}
	defer dbServer.Close()
	if err := dbServer.Ping(); err != nil {
		log.Fatalf("MySQL 服务器不可用: %v", err)
	}
	log.Println("已连接到 MySQL 服务器")

	var exists int
	if err := dbServer.QueryRow(
		"SELECT COUNT(*) FROM INFORMATION_SCHEMA.SCHEMATA WHERE SCHEMA_NAME = ?",
		cfg.Database.Name,
	).Scan(&exists); err != nil {
		log.Fatalf("检查数据库存在性失败: %v", err)
	}
	if exists == 0 {
		createStmt := fmt.Sprintf(
			"CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci",
			cfg.Database.Name,
		)
		if _, err := dbServer.Exec(createStmt); err != nil {
			log.Fatalf("创建数据库失败: %v", err)
		}
		log.Printf("数据库 %s 不存在，已创建成功", cfg.Database.Name)
	} else {
		log.Printf("数据库 %s 已存在", cfg.Database.Name)
	}

	// 2) 使用 GORM 连接目标数据库并自动迁移
	dbDSN := buildDSN(cfg.Database, cfg.Database.Name)
	gormDB, err := gorm.Open(mysql.Open(dbDSN), &gorm.Config{})
	if err != nil {
		log.Fatalf("GORM 连接数据库失败: %v", err)
	}

	// 按照 internal/models/models.go 自动迁移所有表
	if err := gormDB.AutoMigrate(
		&models.User{},
		&models.AuthSession{},
		&models.FriendRequest{},
		&models.Friendship{},
		&models.Block{},
		&models.Room{},
		&models.RoomMember{},
		&models.Message{},
		&models.MessageReceipt{},
		&models.MessageReaction{},
		&models.Attachment{},
	); err != nil {
		log.Fatalf("AutoMigrate 失败: %v", err)
	}
	log.Println("AutoMigrate 完成，数据库结构已根据 models 创建/更新")
}

// buildDSN 以配置构造 DSN；若 dbName 为空则连接到服务器级
func buildDSN(d config.Database, dbName string) string {
	params := url.Values{}
	if d.Params == nil {
		d.Params = map[string]string{}
	}
	if _, ok := d.Params["parseTime"]; !ok {
		params.Set("parseTime", "true")
	}
	if _, ok := d.Params["loc"]; !ok {
		params.Set("loc", "Local")
	}
	if _, ok := d.Params["charset"]; !ok {
		params.Set("charset", "utf8mb4")
	}
	for k, v := range d.Params {
		params.Set(k, v)
	}

	if dbName == "" {
		return fmt.Sprintf("%s:%s@tcp(%s:%d)/?%s", d.User, d.Password, d.Host, d.Port, params.Encode())
	}
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?%s", d.User, d.Password, d.Host, d.Port, dbName, params.Encode())
}