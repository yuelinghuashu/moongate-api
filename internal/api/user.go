package api

import (
	"moongate-api/db"
	"moongate-api/models"
	"net/http"

	"github.com/gin-gonic/gin"
)

// GetCurrentUser 获取当前登录用户信息
func GetCurrentUser(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	var user models.Users
	if err := db.DB.First(&user, userID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":       user.ID,
		"username": user.Username,
		"is_admin": user.IsAdmin,
	})
}
