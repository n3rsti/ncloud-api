package middleware

import (
	"github.com/golang-jwt/jwt/v4"
	"log"
	"ncloud-api/utils/helper"
	"time"
)

var SecretKey = helper.GetEnv("SECRET_KEY", "secret")

type SignedClaims struct {
	Username string `json:"username"`
	jwt.RegisteredClaims
}

func GenerateTokens(username string) (accessToken string, refreshToken string) {
	claims := &SignedClaims{
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Local().Add(time.Hour * time.Duration(24))),
		},
	}

	refreshClaims := &SignedClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Local().Add(time.Hour * time.Duration(168))),
		},
	}

	newToken, err := jwt.NewWithClaims(jwt.SigningMethodHS512, claims).SignedString([]byte(SecretKey))
	if err != nil {
		log.Panic(err)
		return
	}

	newRefreshToken, err := jwt.NewWithClaims(jwt.SigningMethodHS512, refreshClaims).SignedString([]byte(SecretKey))
	if err != nil {
		log.Panic(err)
		return
	}

	return newToken, newRefreshToken
}
