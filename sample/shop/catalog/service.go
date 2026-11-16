package catalog

import (
	"github.com/dangduoc08/ginject/core"
	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/crypto"
)

type StoreService struct {
	Stores StoreRepository
}

func (svc StoreService) NewProvider() core.Provider {
	return svc
}

func (svc StoreService) CreateStore(ownerID, name string) (Store, error) {
	id, err := crypto.UUID()
	if err != nil {
		return Store{}, exception.InternalServerErrorException("failed to create store")
	}

	store := Store{ID: id, OwnerID: ownerID, Name: name}
	if !svc.Stores.CreateStore(store) {
		return Store{}, exception.ConflictException("owner already has a store")
	}

	return store, nil
}

func (svc StoreService) StoreByOwner(ownerID string) (Store, error) {
	store, ok := svc.Stores.StoreByOwner(ownerID)
	if !ok {
		return Store{}, exception.NotFoundException("store not found")
	}

	return store, nil
}

func (svc StoreService) CreateCategory(storeID, name string) (Category, error) {
	id, err := crypto.UUID()
	if err != nil {
		return Category{}, exception.InternalServerErrorException("failed to create category")
	}

	category := Category{ID: id, StoreID: storeID, Name: name}
	if !svc.Stores.CreateCategory(category) {
		return Category{}, exception.NotFoundException("store not found")
	}

	return category, nil
}

func (svc StoreService) Categories(storeID string, page, limit int) Page[Category] {
	categories := svc.Stores.Categories(storeID)

	return newPage(categories, page, limit, len(categories))
}

func (svc StoreService) Category(storeID, categoryID string) (Category, error) {
	category, ok := svc.Stores.Category(storeID, categoryID)
	if !ok {
		return Category{}, exception.NotFoundException("category not found")
	}

	return category, nil
}

func (svc StoreService) UpdateCategory(storeID, categoryID, name string) (Category, error) {
	category := Category{ID: categoryID, StoreID: storeID, Name: name}
	if !svc.Stores.UpdateCategory(category) {
		return Category{}, exception.NotFoundException("category not found")
	}

	return category, nil
}

func (svc StoreService) DeleteCategory(storeID, categoryID string) error {
	if !svc.Stores.DeleteCategory(storeID, categoryID) {
		return exception.NotFoundException("category not found")
	}

	return nil
}

func (svc StoreService) CreateProduct(storeID, categoryID, name string, price float64) (Product, error) {
	id, err := crypto.UUID()
	if err != nil {
		return Product{}, exception.InternalServerErrorException("failed to create product")
	}

	product := Product{ID: id, StoreID: storeID, CategoryID: categoryID, Name: name, Price: price}
	if !svc.Stores.CreateProduct(product) {
		return Product{}, exception.NotFoundException("category not found")
	}

	return product, nil
}

func (svc StoreService) Products(storeID, categoryID string, page, limit int) Page[Product] {
	products := svc.Stores.Products(storeID, categoryID)

	return newPage(products, page, limit, len(products))
}

func (svc StoreService) Product(storeID, categoryID, productID string) (Product, error) {
	product, ok := svc.Stores.Product(storeID, categoryID, productID)
	if !ok {
		return Product{}, exception.NotFoundException("product not found")
	}

	return product, nil
}

func (svc StoreService) UpdateProduct(storeID, categoryID, productID, name string, price float64) (Product, error) {
	product := Product{ID: productID, StoreID: storeID, CategoryID: categoryID, Name: name, Price: price}
	if !svc.Stores.UpdateProduct(product) {
		return Product{}, exception.NotFoundException("product not found")
	}

	return product, nil
}

func (svc StoreService) DeleteProduct(storeID, categoryID, productID string) error {
	if !svc.Stores.DeleteProduct(storeID, categoryID, productID) {
		return exception.NotFoundException("product not found")
	}

	return nil
}
