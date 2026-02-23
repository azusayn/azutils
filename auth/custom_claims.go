package auth

import (
	"errors"

	"github.com/golang-jwt/jwt/v5"
)

const ErrMsgNilCustomClaims = "CustomClaims is nil"

type CustomClaims struct {
	jwt.RegisteredClaims
	// custom fields
	Role string
}

func (c *CustomClaims) requireNonNil() error {
	if c == nil {
		return errors.New(ErrMsgNilCustomClaims)
	}
	return nil
}

func (c *CustomClaims) GetExpirationTime() (*jwt.NumericDate, error) {
	if err := c.requireNonNil(); err != nil {
		return nil, err
	}
	return c.RegisteredClaims.GetExpirationTime()
}

func (c *CustomClaims) GetIssuedAt() (*jwt.NumericDate, error) {
	if err := c.requireNonNil(); err != nil {
		return nil, err
	}
	return c.RegisteredClaims.GetIssuedAt()
}

func (c *CustomClaims) GetNotBefore() (*jwt.NumericDate, error) {
	if err := c.requireNonNil(); err != nil {
		return nil, err
	}
	return c.RegisteredClaims.GetNotBefore()
}

func (c *CustomClaims) GetIssuer() (string, error) {
	if err := c.requireNonNil(); err != nil {
		return "", err
	}
	return c.RegisteredClaims.GetIssuer()
}

func (c *CustomClaims) GetSubject() (string, error) {
	if err := c.requireNonNil(); err != nil {
		return "", err
	}
	return c.RegisteredClaims.GetSubject()
}

func (c *CustomClaims) GetAudience() (jwt.ClaimStrings, error) {
	if err := c.requireNonNil(); err != nil {
		return nil, err
	}
	return c.RegisteredClaims.GetAudience()
}

func (c *CustomClaims) GetRole() (string, error) {
	if err := c.requireNonNil(); err != nil {
		return "", err
	}
	return c.Role, nil
}
