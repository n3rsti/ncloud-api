package auth

import (
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
	"log"
	"ncloud-api/utils/helper"
	"net/http"
	"strings"
	"time"
)

var SecretKey = helper.GetEnv("SECRET_KEY", "secret")

type SignedClaims struct {
	Id string `json:"user_id"`
	jwt.RegisteredClaims
}

func GenerateTokens(userId string) (accessToken, refreshToken string, err error) {
	newToken, err := generateAccessToken(userId)
	if err != nil {
		log.Panic(err)
		return "", "", err
	}

	// Refresh token for 7 days
	refreshClaims := &SignedClaims{
		Id: userId,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Local().Add(time.Hour * time.Duration(168))),
		},
	}

	newRefreshToken, err := jwt.NewWithClaims(jwt.SigningMethodHS512, refreshClaims).SignedString([]byte(SecretKey))
	if err != nil {
		log.Panic(err)
		return "", "", err
	}

	return newToken, newRefreshToken, nil
}

func GenerateAccessTokenFromRefreshToken(refreshToken string) (accessToken string, err error) {
	token, err := jwt.ParseWithClaims(
		refreshToken,
		&SignedClaims{},
		func(token *jwt.Token) (interface{}, error) {
			return []byte(SecretKey), nil
		})

	if err != nil {
		fmt.Println(err)
		err = errors.New("invalid refresh token")
		return
	}

	claims, ok := token.Claims.(*SignedClaims)

	if !ok {
		return
	}

	if claims.ExpiresAt.Unix() < time.Now().Local().Unix() {
		return "", errors.New("refresh token expired")
	}

	newAccessToken, err := generateAccessToken(claims.Id)

	return newAccessToken, nil
}

func generateAccessToken(userId string) (accessToken string, err error) {
	// Access token for 20 minutes
	claims := &SignedClaims{
		Id: userId,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Local().Add(time.Minute * time.Duration(20))),
		},
	}

	newToken, err := jwt.NewWithClaims(jwt.SigningMethodHS512, claims).SignedString([]byte(SecretKey))
	if err != nil {
		return "", err
	}

	return newToken, nil

}

func ValidateToken(signedToken string) (claims *SignedClaims, err error) {
	token, err := jwt.ParseWithClaims(
		signedToken,
		&SignedClaims{},
		func(token *jwt.Token) (interface{}, error) {
			return []byte(SecretKey), nil
		},
	)

	if err != nil {
		return
	}

	claims, ok := token.Claims.(*SignedClaims)
	if !ok {
		err = errors.New("couldn't parse claims")
		return
	}

	if claims.ExpiresAt.Unix() < time.Now().Local().Unix() {
		err = errors.New("token expired")
		return
	}

	if claims.Id == "" {
		err = errors.New("empty id")
		return
	}

	return
}

// ExtractClaims
//
// # Return JWT claims as SignedClaims
//
// This function does not contain any checks for validity
// It should only be used after successfully passing ValidateToken method
func ExtractClaims(signedToken string) *SignedClaims {
	token, err := jwt.ParseWithClaims(
		signedToken,
		&SignedClaims{},
		func(token *jwt.Token) (interface{}, error) {
			return []byte(SecretKey), nil
		},
	)

	if err != nil {
		log.Panic(err)
		return nil
	}

	claims, ok := token.Claims.(*SignedClaims)
	if !ok {
		return nil
	}

	return claims
}

// ExtractClaimsFromContext
//
// # Return JWT claims from gin.Context as SignedClaims
//
// This function does not contain any checks for validity
// It should only be used after successfully passing ValidateToken method
func ExtractClaimsFromContext(c *gin.Context) *SignedClaims {
	token := c.GetHeader("Authorization")
	token = token[len("Bearer "):]

	return ExtractClaims(token)
}

func Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("Authorization")

		if !strings.HasPrefix(token, "Bearer ") {
			c.IndentedJSON(http.StatusUnauthorized, gin.H{
				"error": "no access token",
			})
			c.Header("WWW-Authenticate", "invalid access token")
			c.Abort()
			return
		}

		token = token[len("Bearer "):]

		_, err := ValidateToken(token)

		if err != nil {
			c.IndentedJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid access token",
			})
			c.Header("WWW-Authenticate", "invalid access token")
			c.Abort()
			return
		}

		c.Next()
	}
}
