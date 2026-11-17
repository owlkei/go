package accounts

import (
	"sync"

	"github.com/dangduoc08/ginject"
)

type userRecord struct {
	user         User
	passwordHash string
}

type userTable struct {
	mu      sync.RWMutex
	byID    map[string]userRecord
	byEmail map[string]string
}

type UserRepository struct {
	Table *userTable
}

func (repo UserRepository) NewProvider() ginject.Provider {
	repo.Table = &userTable{
		byID:    map[string]userRecord{},
		byEmail: map[string]string{},
	}

	return repo
}

func (repo UserRepository) Create(user User, passwordHash string) bool {
	repo.Table.mu.Lock()
	defer repo.Table.mu.Unlock()

	if _, ok := repo.Table.byEmail[user.Email]; ok {
		return false
	}

	repo.Table.byID[user.ID] = userRecord{user: user, passwordHash: passwordHash}
	repo.Table.byEmail[user.Email] = user.ID

	return true
}

func (repo UserRepository) ByEmail(email string) (User, string, bool) {
	repo.Table.mu.RLock()
	defer repo.Table.mu.RUnlock()

	record, ok := repo.Table.byID[repo.Table.byEmail[email]]

	return record.user, record.passwordHash, ok
}

func (repo UserRepository) ByID(id string) (User, bool) {
	repo.Table.mu.RLock()
	defer repo.Table.mu.RUnlock()

	record, ok := repo.Table.byID[id]

	return record.user, ok
}
