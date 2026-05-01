package auth

import (
	"crypto/ed25519"
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

func GenerateAccessToken(
	method jwt.SigningMethod,
	key any,
	issuer string,
	duration time.Duration,
	version string,
	userId int32,
	role string,
) (string, error) {
	now := time.Now()
	token := jwt.NewWithClaims(method, &CustomClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatInt(int64(userId), 10),
			Issuer:    issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(duration)),
			Audience:  jwt.ClaimStrings{string(audienceTypeAccessTokenUser)},
			// ID is used for identifying a token.
			ID: uuid.NewString(),
		},
		Role: role,
	})
	// kid is the key version used for key rotation.
	token.Header["kid"] = version
	signedToken, err := token.SignedString(key)
	if err != nil {
		return "", errors.Wrapf(err, "failed to sign token")
	}
	return signedToken, nil
}

// ValidateAccessToken validates an access token and returns the user ID and role.
// It only supports using RSA, Ed25519 and HMAC as the signing method.
func ValidateAccessToken(key any, token string, expectedIssuer string) (int32, string, error) {
	claims := &CustomClaims{}
	_, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		// NOTE: this switch makes sure the program is using the right verification key
		// to prevent algorithm confusion attack.
		// ref: https://portswigger.net/web-security/jwt/algorithm-confusion
		switch t.Method.(type) {
		case *jwt.SigningMethodRSA:
			if _, ok := key.(*rsa.PublicKey); ok {
				return key, nil
			}
		case *jwt.SigningMethodEd25519:
			if _, ok := key.(ed25519.PublicKey); ok {
				return key, nil
			}
		case *jwt.SigningMethodHMAC:
			if v, ok := key.([]byte); ok {
				return v, nil
			}
		default:
		}
		return nil, errors.Errorf("signing method not supported: %q", t.Method.Alg())
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
