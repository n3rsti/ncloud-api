package auth

import (
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"log"
	"ncloud-api/utils/helper"
	"net/http"
	"strings"
	"time"
)

var SecretKey = helper.GetEnv("SECRET_KEY", "secret")
var FileSecretKey = helper.GetEnv("FILE_SECRET_KEY", "file_secret")

type SignedClaims struct {
	Id    string `json:"user_id"`
	Token string `json:"token,omitempty"`
	jwt.RegisteredClaims
}

type FileClaims struct {
	Id              string   `json:"id"`
	Permissions     []string `json:"permissions"`
	ParentDirectory string   `json:"parent_directory,omitempty"`
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
		Id:    userId,
		Token: "refresh",
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

	if claims.Token != "refresh" {
		return "", errors.New("provided token is not refresh token")
	}

	newAccessToken, _ := generateAccessToken(claims.Id)

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

// GenerateFileAccessKey
// generates access key for file or directory with parameters:
//
//	id, permissions, parentDirectory (optional)
//
// parentDirectory should only be used for files and doesn't need to be verified before
//
// although parentDirectory is optional as function argument, it is MANDATORY to use parentDirectory for files
//
//	fileAccessKey, err := auth.GenerateFileAccessKey(fileId, permissions, directory)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Println(fileAccessKey.id)
//	fmt.Println(fileAccessKey.permissions)
func GenerateFileAccessKey(id string, permissions []string, parentDirectory ...string) (string, error) {
	// Verify id format
	if _, err := primitive.ObjectIDFromHex(id); err != nil {
		return "", err
	}
	claims := &FileClaims{
		Id:          id,
		Permissions: permissions,
	}
	if len(parentDirectory) > 0 {
		// verify parent directory format
		if _, err := primitive.ObjectIDFromHex(parentDirectory[0]); err != nil {
			return "", err
		}

		claims.ParentDirectory = parentDirectory[0]
	}

	newToken, err := jwt.NewWithClaims(jwt.SigningMethodHS512, claims).SignedString([]byte(FileSecretKey))
	if err != nil {
		return "", err
	}

	return newToken, nil
}

func ValidateAccessKey(accessKey string) (claims *FileClaims, valid bool) {
	token, err := jwt.ParseWithClaims(
		accessKey,
		&FileClaims{},
		func(token *jwt.Token) (interface{}, error) {
			return []byte(FileSecretKey), nil
		})

	if err != nil {
		return &FileClaims{}, false
	}

	claims, ok := token.Claims.(*FileClaims)

	if !ok {
		return &FileClaims{}, false
	}

	return claims, true
}

func ValidateAccessKeyWithId(accessKey, id string) bool{
	claims, valid := ValidateAccessKey(accessKey)
	if !valid {
		return false
	}

	return claims.Id == id
}

// ValidatePermissions MUST only be used after ValidateAccessKey function
func ValidatePermissions(accessKey, permission string) bool {
	token, _ := jwt.ParseWithClaims(
		accessKey,
		&FileClaims{},
		func(token *jwt.Token) (interface{}, error) {
			return []byte(FileSecretKey), nil
		})

	claims, _ := token.Claims.(*FileClaims)
	return helper.StringArrayContains(claims.Permissions, permission)
}

func ValidatePermissionsFromClaims(claims *FileClaims, permission string) bool{
	return helper.StringArrayContains(claims.Permissions, permission)
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
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "no access token",
			})
			c.Header("WWW-Authenticate", "invalid access token")
			c.Abort()
			return
		}

		token = token[len("Bearer "):]

		claims, err := ValidateToken(token)

		if err != nil {
			fmt.Print("XD")
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "invalid access token",
			})
			c.Header("WWW-Authenticate", "invalid access token")
			c.Abort()
			return
		}

		if claims.Token == "refresh" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "provided token is refresh token (should be access token)",
			})
			c.Header("WWW-Authenticate", "provided token is refresh token (should be access token)")
			c.Abort()
			return
		}

		c.Next()
	}
}
