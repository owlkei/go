package accounts

import (
	"context"
	"strings"

	"go.owlky.com/owlky"
)

type contextKey string

const currentUserKey contextKey = "shop.currentUser"

type AuthGuard struct {
	owlky.Guard
	UserService
}

func (instance AuthGuard) NewGuard() AuthGuard {
	return instance
}

func (instance AuthGuard) CanActivate(c owlky.HTTPContext) bool {
	token := strings.TrimPrefix(c.Header().Get("Authorization"), "Bearer ")
	if token == "" {
		return false
	}

	user, ok := instance.UserService.UserBySession(token)
	if !ok {
		return false
	}

	c.Request = c.WithContext(context.WithValue(c.Context(), currentUserKey, user))

	return true
}

func CurrentUser(c owlky.HTTPContext) User {
	user, _ := c.Context().Value(currentUserKey).(User)

	return user
}
