package models

import "gorm.io/gorm"

type Users struct {
	gorm.Model
	GitHubID string `json:"github_id" gorm:"uniqueIndex;column:github_id"`
	Username string `json:"username" gorm:"not null"`
	IsAdmin  bool   `json:"is_admin" gorm:"default:false"`
}
