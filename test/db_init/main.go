package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/url"

	"ququchat/internal/config"

	_ "github.com/go-sql-driver/mysql"
)

func main() {
	cfg, err := config.LoadDefault()
	if err != nil {
		log.Fatalf("读取配置失败: %v", err)
	}

	if cfg.Database.Driver != "mysql" {
		log.Fatalf("当前仅支持 mysql，配置为: %s", cfg.Database.Driver)
	}
	if cfg.Database.User == "" {
		log.Fatalf("数据库用户未配置")
	}
	if cfg.Database.Password == "" {
		log.Println("提示: 配置中的 password 为空，请在 internal/config/config.yaml 填写你的密码")
	}

	// 连接到服务器级(不选库)，用于检测并创建数据库
	serverDSN := buildDSN(cfg.Database, "")
	db, err := sql.Open("mysql", serverDSN)
	if err != nil {
		log.Fatalf("连接到 MySQL 服务器失败: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		log.Fatalf("MySQL 服务器不可用: %v", err)
	}
	log.Println("已连接到 MySQL 服务器")

	// 检查数据库是否存在
	var exists int
	if err := db.QueryRow(
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
		if _, err := db.Exec(createStmt); err != nil {
			log.Fatalf("创建数据库失败: %v", err)
		}
		log.Printf("数据库 %s 不存在，已创建成功", cfg.Database.Name)
	} else {
		log.Printf("数据库 %s 已存在", cfg.Database.Name)
	}

	// 连接到指定数据库并做一次简单校验
	dbDSN := buildDSN(cfg.Database, cfg.Database.Name)
	db2, err := sql.Open("mysql", dbDSN)
	if err != nil {
		log.Fatalf("连接到数据库失败: %v", err)
	}
	defer db2.Close()
	if err := db2.Ping(); err != nil {
		log.Fatalf("数据库不可用: %v", err)
	}
	log.Printf("已连接到数据库 %s", cfg.Database.Name)

	// 基础校验: 简单查询
	var one int
	if err := db2.QueryRow("SELECT 1").Scan(&one); err != nil {
		log.Fatalf("基础查询失败: %v", err)
	}
	log.Printf("基础查询成功，返回值: %d", one)

	log.Println("测试完成")
}

// buildDSN 以配置构造 DSN；若 dbName 为空则连接到服务器级
func buildDSN(d config.Database, dbName string) string {
	params := url.Values{}
	// 默认参数
	if d.Params == nil {
		// 保护性创建避免 nil map
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
