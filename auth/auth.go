package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/pkg/errors"
)

const (
	HttpHeaderAuthorization string = "authorization"
	HttpHeaderBearer        string = "bearer"
)

type AudienceType string

const (
	audienceTypeAccessTokenUser AudienceType = "access_token_user"
)

func GenerateAccessToken(userId int, privateKey *rsa.PrivateKey, issuer string, duration time.Duration) (string, error) {
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{
		Subject:   strconv.Itoa(userId),
		Issuer:    issuer,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(duration)),
		Audience:  jwt.ClaimStrings{string(audienceTypeAccessTokenUser)},
		// actually this is only used for key rotation.
		ID: uuid.NewString(),
	})
	signedToken, err := token.SignedString(privateKey)
	if err != nil {
		return "", errors.Wrapf(err, "failed to sign token")
	}
	return signedToken, nil
}

// validate access token and return user id.
func ValidateAccessToken(token string, publicKey *rsa.PublicKey, issuer string) (int, error) {
	claims := &jwt.RegisteredClaims{}
	_, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		if alg := t.Method.Alg(); alg != jwt.SigningMethodRS256.Name {
			return nil, errors.Errorf("sighing method not supported: %q", alg)
		}
		return publicKey, nil
	})
	if err != nil {
		return 0, errors.Wrapf(err, "failed to parse claims")
	}

	if len(claims.Audience) != 1 || claims.Audience[0] != string(audienceTypeAccessTokenUser) {
		return 0, errors.New(fmt.Sprintf("audience type not supported: %q", claims.Audience[0]))
	}

	if claims.Issuer != issuer {
		return 0, errors.New(fmt.Sprintf("wrong issuer: %q", claims.Issuer))
	}

	userId, err := strconv.Atoi(claims.Subject)
	if err != nil {
		return 0, err
	}

	return userId, nil
}

func GeneratePrivateKey() (*rsa.PrivateKey, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	return privateKey, nil
}

func CheckUsername(username string) error {
	if len(username) == 0 {
		return errors.New("username empty")
	}
	if len(username) < 6 {
		return errors.New("username too short")
	}
	if len(username) > 15 {
		return errors.New("username too long")
	}
	if matched, _ := regexp.MatchString(`^[a-zA-Z0-9_@]+$`, username); !matched {
		return errors.New("invalid username")
	}
	return nil
}
