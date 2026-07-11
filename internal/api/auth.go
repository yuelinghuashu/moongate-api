package api

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"moongate-api/db"
	"moongate-api/models"
	"moongate-api/pkg"
	"net/http"
	"net/url"
	"os"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func GitHubLogin(c *gin.Context) {
	log.Println("🔍 发起 GitHub 登录")

	state, err := generateState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "系统错误"})
		return
	}

	c.SetCookie("oauth_state", state, 600, "/", "", false, true)

	params := url.Values{}
	params.Set("client_id", os.Getenv("GITHUB_KEY"))
	params.Set("redirect_uri", os.Getenv("GITHUB_CALLBACK_URL"))
	params.Set("state", state)
	params.Set("scope", "read:user")

	loginURL := "https://github.com/login/oauth/authorize?" + params.Encode()
	c.Redirect(http.StatusFound, loginURL)
}

func GitHubCallback(c *gin.Context) {
	log.Println("🔍 处理 GitHub 回调")

	code := c.Query("code")
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 code"})
		return
	}

	// 验证 state
	if state := c.Query("state"); state != "" {
		if cookieState, _ := c.Cookie("oauth_state"); cookieState != "" && state != cookieState {
			c.JSON(http.StatusBadRequest, gin.H{"error": "state 验证失败"})
			return
		}
	}

	// 获取用户信息
	ghUser, err := fetchGitHubUser(code)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	githubID, _ := ghUser["id"].(float64)
	login, _ := ghUser["login"].(string)

	// 查找或创建用户
	user, err := findOrCreateUser(int(githubID), login)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 签发 JWT
	token, err := pkg.GenerateJWT(user.ID, user.Username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成登录凭证失败"})
		return
	}

	// ✅✅✅ 关键：重定向回前端 ✅✅✅
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "http://localhost:3000"
	}

	userJSON, _ := json.Marshal(gin.H{
		"id":       user.ID,
		"username": user.Username,
		"is_admin": user.IsAdmin,
	})

	redirectURL := fmt.Sprintf("%s/callback?token=%s&user=%s",
		frontendURL,
		token,
		url.QueryEscape(string(userJSON)),
	)

	log.Printf("🔍 重定向到前端: %s", redirectURL)
	c.Redirect(http.StatusFound, redirectURL)
}

// generateState 生成随机字符串
func generateState() (string, error) {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// fetchGitHubUser 用 code 换取用户信息
func fetchGitHubUser(code string) (map[string]interface{}, error) {
	// 换 token
	params := url.Values{}
	params.Set("client_id", os.Getenv("GITHUB_KEY"))
	params.Set("client_secret", os.Getenv("GITHUB_SECRET"))
	params.Set("code", code)

	resp, err := http.PostForm("https://github.com/login/oauth/access_token", params)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	vals, _ := url.ParseQuery(string(body))
	token := vals.Get("access_token")
	if token == "" {
		return nil, errors.New("获取 token 失败")
	}

	// 拿用户信息
	req, _ := http.NewRequest("GET", "https://api.github.com/user", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var user map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&user)
	return user, nil
}

// findOrCreateUser 查找或创建用户
func findOrCreateUser(githubID int, login string) (*models.Users, error) {
	database := db.GetDB()
	if database == nil {
		return nil, errors.New("数据库未初始化")
	}

	var user models.Users

	result := database.Where("github_id = ?", strconv.Itoa(githubID)).First(&user)
	if result.Error == nil {
		return &user, nil
	}
	if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, result.Error
	}

	username := login
	if username == "" {
		username = "github_user"
	}

	var existing models.Users
	if database.Where("username = ?", username).First(&existing).Error == nil {
		username = username + "_" + strconv.Itoa(githubID)[:4]
	}

	newUser := models.Users{
		Username: username,
		GitHubID: strconv.Itoa(githubID),
	}
	if err := database.Create(&newUser).Error; err != nil {
		return nil, err
	}
	return &newUser, nil
}
