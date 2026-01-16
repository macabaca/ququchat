package userinfo

import (
	"gorm.io/gorm"

	"ququchat/internal/models"
)

func GetUserCodeByID(db *gorm.DB, userID string) (int64, error) {
	var u models.User
	if err := db.Select("user_code").Where("id = ?", userID).First(&u).Error; err != nil {
		return 0, err
	}
	return u.UserCode, nil
}

