package owlky

import (
	"context"

	"go.owlky.com/owlky/internal/aggregation"
	"go.owlky.com/owlky/internal/common"
	"go.owlky.com/owlky/internal/core"
	"go.owlky.com/owlky/internal/ctx"
	"go.owlky.com/owlky/internal/versioning"
)

type (
	App           = *core.App
	Module        = *core.Module
	Controller    = core.Controller
	Provider      = core.Provider
	WSOptions     = core.WSOptions
	RenderOptions = core.RenderOptions
	RetryOptions  = core.RetryOptions

	AppOptions           = core.AppOptions
	ServerOptions        = core.ServerOptions
	TLSOptions           = core.TLSOptions
	HTTPOptions          = core.HTTPOptions
	ProxyOptions         = core.ProxyOptions
	ProxyHeader          = core.ProxyHeader
	LoadShedOptions      = core.LoadShedOptions
	HTTPSRedirectOptions = core.HTTPSRedirectOptions
	HeadersOptions       = core.HeadersOptions
	CORSOptions          = core.CORSOptions
	CSRFOptions          = core.CSRFOptions
	CompressOptions      = core.CompressOptions
	ByteSize             = core.ByteSize

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

	VersioningOptions = versioning.Options
	VersioningType    = versioning.Type

	PathStyle = common.PathStyle
)

func ModuleBuilder() *core.ModuleBuilder {
	return core.NewModuleBuilder()
}

func New(opts *AppOptions) App {
	return core.New(opts)
}

func Retry(ctx context.Context, opts *core.RetryOptions, fn func(ctx context.Context) error) error {
	return core.Retry(ctx, opts, fn)
}

const (
	VersioningQuery     = versioning.TypeQuery
	VersioningHeader    = versioning.TypeHeader
	VersioningCustom    = versioning.TypeCustom
	VersioningMediaType = versioning.TypeMediaType
)

const (
	PathStyleKebab     = common.PathStyleKebab
	PathStyleAsWritten = common.PathStyleAsWritten
)
