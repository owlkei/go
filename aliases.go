package ginject

import (
	"github.com/dangduoc08/ginject/internal/aggregation"
	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/core"
	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/versioning"
)

type (
	App           = *core.App
	Module        = *core.Module
	Controller    = core.Controller
	Provider      = core.Provider
	ServerOptions = core.ServerOptions
	WSOptions     = core.WSOptions

	HTTP             = common.HTTP
	WS               = common.WS
	Guard            = common.Guard
	Interceptor      = common.Interceptor
	ExceptionFilter  = common.ExceptionFilter
	Middleware       = common.Middleware
	MiddlewareFn     = common.MiddlewareFn
	ArgumentMetadata = common.ArgumentMetadata
	Publisher        = common.Publisher

	HTTPContext = *ctx.HTTPContext
	WSContext   = *ctx.WSContext
	Body        = ctx.Body
	Form        = ctx.Form
	File        = ctx.File
	Query       = ctx.Query
	Header      = ctx.Header
	Param       = ctx.Param
	WSPayload   = ctx.WSPayload
	Map         = ctx.Map
	Next        = ctx.Next
	Redirect    = ctx.Redirect
	FieldLevel  = ctx.FieldLevel
	DataFile    = *ctx.DataFile

	Aggregation = *aggregation.Aggregation

	Versioning = versioning.Versioning
)

func ModuleBuilder() *core.ModuleBuilder {
	return core.NewModuleBuilder()
}

func New() App {
	return core.New()
}
