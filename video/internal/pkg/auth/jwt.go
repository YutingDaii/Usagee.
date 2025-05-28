package auth

import (
	"errors"
	"os"
	"time"
	jwt "github.com/golang-jwt/jwt/v5"
)

// 自定义的 JWT Claims（载荷）
  type CustomClaims struct {
	jwt.RegisteredClaims
  }
  
  // 生成 JWT Token
  func GenerateToken(userID int64) (string, error) {
	claims := CustomClaims{
	  RegisteredClaims: jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(12 * time.Hour)), // 过期时间
		Issuer:    "Usagee.User",                                  // 签发者
	  },
	}
	
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		return "", errors.New("JWT_SECRET environment variable is not set")
	}
	return token.SignedString([]byte(os.Getenv("JWT_SECRET")))
  }
