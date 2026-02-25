package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"fmt"
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

func GenerateAccessToken(userId int32, privateKey *rsa.PrivateKey, issuer string, role string, duration time.Duration) (string, error) {
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, &CustomClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatInt(int64(userId), 10),
			Issuer:    issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(duration)),
			Audience:  jwt.ClaimStrings{string(audienceTypeAccessTokenUser)},
			// TODO: key rotation.
			ID: uuid.NewString(),
		},
		Role: role,
	})
	signedToken, err := token.SignedString(privateKey)
	if err != nil {
		return "", errors.Wrapf(err, "failed to sign token")
	}
	return signedToken, nil
}

// validate access token and return user id & user role.
func ValidateAccessToken(token string, publicKey *rsa.PublicKey, expectedIssuer string) (int32, string, error) {
	claims := &CustomClaims{}
	_, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		if alg := t.Method.Alg(); alg != jwt.SigningMethodRS256.Name {
			return nil, errors.Errorf("sighing method not supported: %q", alg)
		}
		return publicKey, nil
	})
	if err != nil {
		return 0, "", errors.Wrapf(err, "failed to parse claims")
	}

	aud, err := claims.GetAudience()
	if err != nil {
		return 0, "", err
	}
	if len(aud) != 1 || aud[0] != string(audienceTypeAccessTokenUser) {
		return 0, "", fmt.Errorf("invalid audience type: %v", aud)
	}

	issuer, err := claims.GetIssuer()
	if err != nil {
		return 0, "", err
	}
	if issuer != expectedIssuer {
		return 0, "", fmt.Errorf("wrong issuer: %q", issuer)
	}

	sub, err := claims.GetSubject()
	if err != nil {
		return 0, "", err
	}
	userId, err := strconv.Atoi(sub)
	if err != nil {
		return 0, "", err
	}

	role, err := claims.GetRole()
	if err != nil {
		return 0, "", err
	}

	return int32(userId), role, nil
}

func GeneratePrivateKey() (*rsa.PrivateKey, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	return privateKey, nil
}
