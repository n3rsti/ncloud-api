package auth

import (
	"errors"
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
	Username string `json:"username"`
	jwt.RegisteredClaims
}

func GenerateTokens(username string) (accessToken, refreshToken string, err error) {
	newToken, err := generateAccessToken(username)
	if err != nil {
		log.Panic(err)
		return "", "", err
	}

	// Refresh token for 7 days
	refreshClaims := &SignedClaims{
		Username: username,
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
		log.Panic(err)
		return
	}

	claims, ok := token.Claims.(*SignedClaims)

	if !ok {
		return
	}

	if claims.ExpiresAt.Unix() < time.Now().Local().Unix() {
		return "", errors.New("refresh token expired")
	}

	newAccessToken, err := generateAccessToken(claims.Username)

	return newAccessToken, nil
}

func generateAccessToken(username string) (accessToken string, err error) {
	// Access token for 20 minutes
	claims := &SignedClaims{
		Username: username,
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

func ValidateToken(signedToken string) (claims *SignedClaims, err error){
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



	return
}

// ExtractClaims
//
// Return JWT claims as SignedClaims
//
// This function does not contain any checks for validity
// It should only be used after successfully passing ValidateToken method
//
//
func ExtractClaims(signedToken string) *SignedClaims{
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
// Return JWT claims from gin.Context as SignedClaims
//
// This function does not contain any checks for validity
// It should only be used after successfully passing ValidateToken method
//
//
func ExtractClaimsFromContext(c *gin.Context) *SignedClaims {
	token := c.GetHeader("Authorization")
	token = token[len("Bearer "):]

	return ExtractClaims(token)
}


func Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("Authorization")

		if !strings.HasPrefix(token, "Bearer ") {
			c.IndentedJSON(http.StatusBadRequest, gin.H{
				"error": "Invalid access token",
			})
			c.Abort()
			return
		}

		token = token[len("Bearer "):]

		_, err := ValidateToken(token)


		if err != nil {
			c.IndentedJSON(http.StatusBadRequest, gin.H{
				"error": "Invalid access token",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
