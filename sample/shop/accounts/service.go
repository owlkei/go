package accounts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/dangduoc08/ginject"
	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/modules/cache"
	"github.com/dangduoc08/ginject/sample/shared"
)

const (
	sessionTTL     = 24 * time.Hour
	sessionKeyPath = "session:"
)

type UserService struct {
	Users UserRepository
	Cache cache.CacheService
}

func (svc UserService) NewProvider() ginject.Provider {
	return svc
}

func (svc UserService) Register(email, name, password string) (User, error) {
	id, err := shared.UUID()
	if err != nil {
		return User{}, exception.InternalServerErrorException("failed to create user")
	}

	user := User{ID: id, Email: email, Name: name}
	if !svc.Users.Create(user, hashPassword(password)) {
		return User{}, exception.ConflictException("email is already registered")
	}

	return user, nil
}

func (svc UserService) Authenticate(email, password string) (User, string, error) {
	user, passwordHash, ok := svc.Users.ByEmail(email)
	if !ok || passwordHash != hashPassword(password) {
		return User{}, "", exception.UnauthorizedException("invalid email or password")
	}

	token, err := shared.UUID()
	if err != nil {
		return User{}, "", exception.InternalServerErrorException("failed to start session")
	}

	if err := svc.Cache.Set(context.Background(), sessionKey(token), []byte(user.ID), sessionTTL); err != nil {
		return User{}, "", exception.InternalServerErrorException("failed to start session")
	}

	return user, token, nil
}

func (svc UserService) Logout(token string) {
	_ = svc.Cache.Delete(context.Background(), sessionKey(token))
}

func (svc UserService) UserBySession(token string) (User, bool) {
	userID, ok := svc.Cache.Get(context.Background(), sessionKey(token))
	if !ok {
		return User{}, false
	}

	return svc.Users.ByID(string(userID))
}

func sessionKey(token string) string {
	return sessionKeyPath + token
}

func hashPassword(password string) string {
	sum := sha256.Sum256([]byte(password))
	return hex.EncodeToString(sum[:])
}
