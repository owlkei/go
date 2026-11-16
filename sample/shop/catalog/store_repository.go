package catalog

import (
	"slices"
	"sync"

	"github.com/dangduoc08/ginject/core"
)

type catalogTables struct {
	mu                 sync.RWMutex
	stores             map[string]Store
	storeByOwner       map[string]string
	categories         map[string]Category
	categoryIDsByStore map[string][]string
	products           map[string]Product
	productIDsByParent map[string][]string
}

type StoreRepository struct {
	Tables *catalogTables
}

func (repo StoreRepository) NewProvider() core.Provider {
	repo.Tables = &catalogTables{
		stores:             map[string]Store{},
		storeByOwner:       map[string]string{},
		categories:         map[string]Category{},
		categoryIDsByStore: map[string][]string{},
		products:           map[string]Product{},
		productIDsByParent: map[string][]string{},
	}

	return repo
}

func (repo StoreRepository) CreateStore(store Store) bool {
	t := repo.Tables
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, ok := t.storeByOwner[store.OwnerID]; ok {
		return false
	}

	t.stores[store.ID] = store
	t.storeByOwner[store.OwnerID] = store.ID

	return true
}

func (repo StoreRepository) StoreByOwner(ownerID string) (Store, bool) {
	t := repo.Tables
	t.mu.RLock()
	defer t.mu.RUnlock()

	store, ok := t.stores[t.storeByOwner[ownerID]]

	return store, ok
}

func (repo StoreRepository) CreateCategory(category Category) bool {
	t := repo.Tables
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, ok := t.stores[category.StoreID]; !ok {
		return false
	}

	t.categories[category.ID] = category
	t.categoryIDsByStore[category.StoreID] = append(t.categoryIDsByStore[category.StoreID], category.ID)

	return true
}

func (repo StoreRepository) Categories(storeID string) []Category {
	t := repo.Tables
	t.mu.RLock()
	defer t.mu.RUnlock()

	ids := t.categoryIDsByStore[storeID]
	categories := make([]Category, 0, len(ids))
	for _, id := range ids {
		categories = append(categories, t.categories[id])
	}

	return categories
}

func (repo StoreRepository) Category(storeID, categoryID string) (Category, bool) {
	t := repo.Tables
	t.mu.RLock()
	defer t.mu.RUnlock()

	category, ok := t.categories[categoryID]

	return category, ok && category.StoreID == storeID
}

func (repo StoreRepository) UpdateCategory(category Category) bool {
	t := repo.Tables
	t.mu.Lock()
	defer t.mu.Unlock()

	if current, ok := t.categories[category.ID]; !ok || current.StoreID != category.StoreID {
		return false
	}

	t.categories[category.ID] = category

	return true
}

func (repo StoreRepository) DeleteCategory(storeID, categoryID string) bool {
	t := repo.Tables
	t.mu.Lock()
	defer t.mu.Unlock()

	if current, ok := t.categories[categoryID]; !ok || current.StoreID != storeID {
		return false
	}

	for _, id := range t.productIDsByParent[categoryID] {
		delete(t.products, id)
	}
	delete(t.productIDsByParent, categoryID)
	delete(t.categories, categoryID)
	t.categoryIDsByStore[storeID] = removeID(t.categoryIDsByStore[storeID], categoryID)

	return true
}

func (repo StoreRepository) CreateProduct(product Product) bool {
	t := repo.Tables
	t.mu.Lock()
	defer t.mu.Unlock()

	if category, ok := t.categories[product.CategoryID]; !ok || category.StoreID != product.StoreID {
		return false
	}

	t.products[product.ID] = product
	t.productIDsByParent[product.CategoryID] = append(t.productIDsByParent[product.CategoryID], product.ID)

	return true
}

func (repo StoreRepository) Products(storeID, categoryID string) []Product {
	t := repo.Tables
	t.mu.RLock()
	defer t.mu.RUnlock()

	ids := t.productIDsByParent[categoryID]
	products := make([]Product, 0, len(ids))
	for _, id := range ids {
		if product := t.products[id]; product.StoreID == storeID {
			products = append(products, product)
		}
	}

	return products
}

func (repo StoreRepository) Product(storeID, categoryID, productID string) (Product, bool) {
	t := repo.Tables
	t.mu.RLock()
	defer t.mu.RUnlock()

	product, ok := t.products[productID]

	return product, ok && product.StoreID == storeID && product.CategoryID == categoryID
}

func (repo StoreRepository) UpdateProduct(product Product) bool {
	t := repo.Tables
	t.mu.Lock()
	defer t.mu.Unlock()

	current, ok := t.products[product.ID]
	if !ok || current.StoreID != product.StoreID || current.CategoryID != product.CategoryID {
		return false
	}

	t.products[product.ID] = product

	return true
}

func (repo StoreRepository) DeleteProduct(storeID, categoryID, productID string) bool {
	t := repo.Tables
	t.mu.Lock()
	defer t.mu.Unlock()

	current, ok := t.products[productID]
	if !ok || current.StoreID != storeID || current.CategoryID != categoryID {
		return false
	}

	delete(t.products, productID)
	t.productIDsByParent[categoryID] = removeID(t.productIDsByParent[categoryID], productID)

	return true
}

func removeID(ids []string, id string) []string {
	return slices.DeleteFunc(ids, func(candidate string) bool { return candidate == id })
}
