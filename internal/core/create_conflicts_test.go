package core

import (
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/test"
)

type cfByIDController struct{ common.HTTP }

func (c cfByIDController) NewController() Controller { return c }
func (c cfByIDController) READ_cf_BY_id() string     { return "id" }
func (c cfByIDController) READ_cf_BY_name() string   { return "name" }

func TestCreate_SameShapeDifferentParamNamesConflicts(t *testing.T) {
	freshProcess(t, func(t *testing.T) {
		text, panicked := createPanicText(func() {
			New().Create(NewModuleBuilder().Controllers(cfByIDController{}).Build())
		})
		if !panicked || !strings.Contains(text, "route conflict") || !strings.Contains(text, "READ_cf_BY_id") || !strings.Contains(text, "READ_cf_BY_name") {
			t.Error(test.DiffMessage(text, "route conflict: READ_cf_BY_… ... READ_cf_BY_…", "I6: two routes that differ only in parameter names conflict at bootstrap"))
		}
	})
}

type cfShapesController struct{ common.HTTP }

func (c cfShapesController) NewController() Controller       { return c }
func (c cfShapesController) READ_cfs_BY_id() string          { return "a" }
func (c cfShapesController) READ_cfs_BY_id_AND_name() string { return "b" }
func (c cfShapesController) UPDATE_cfs_BY_id() string        { return "c" }
func (c cfShapesController) READ_cfs_all() string            { return "d" }

func TestCreate_DifferentShapesDoNotConflict(t *testing.T) {
	freshProcess(t, func(t *testing.T) {
		text, panicked := createPanicText(func() {
			New().Create(NewModuleBuilder().Controllers(cfShapesController{}).Build())
		})
		if panicked {
			t.Error(test.DiffMessage(text, "no panic", "I6: routes with a different shape, method or static segment do not conflict"))
		}
	})
}
