package main

import (
	"log"

	"ququchat/internal/config"
	"ququchat/internal/models"
	"ququchat/internal/server/db"
)

func main() {
	cfg, err := config.LoadDefault()
	if err != nil {
		log.Fatalf("读取配置失败: %v", err)
	}
	if cfg.Database.User == "" {
		log.Fatalf("数据库用户未配置")
	}
	if cfg.Database.Password == "" {
		log.Println("提示: 配置中的 password 为空，请在 internal/config/config.yaml 填写你的密码")
	}

	gormDB, err := db.OpenGorm(cfg.Database)
	if err != nil {
		log.Fatalf("GORM 连接数据库失败: %v", err)
	}

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
