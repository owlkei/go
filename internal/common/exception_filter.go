package common

import (
	"errors"
	"net/http"
	"reflect"

	"go.owlkei.com/owlkei/exception"
	"go.owlkei.com/owlkei/internal/color"
)

const ExceptionFilterMethodName = "Catch"

type ExceptionFilterItem struct {
	HTTP HTTPExceptionFilterItem
	WS   WSExceptionFilterItem
}

type exceptionFilterHandler struct {
	exceptionFilterable any
	handlers            []any
}

type ExceptionFilter struct {
	exceptionFilterHandlers []exceptionFilterHandler
}

func (e *ExceptionFilter) BindExceptionFilter(exceptionFilter any, handlers ...any) *ExceptionFilter {
	exceptionFilterHandler := exceptionFilterHandler{
		exceptionFilterable: exceptionFilter,
		handlers:            handlers,
	}

	e.exceptionFilterHandlers = append(e.exceptionFilterHandlers, exceptionFilterHandler)
	return e
}

func ExceptionFilterShapeError(exceptionFilterable any) error {
	return errors.New(color.FmtRed(
		"invalid exception filter: %v has no %s method usable as an exception filter",
		reflect.TypeOf(exceptionFilterable),
		ExceptionFilterMethodName,
	))
}

func NormalizeRecovered(rec any) *exception.Exception {
	switch arg := rec.(type) {
	case *exception.Exception:
		if arg != nil {
			return arg
		}
	case exception.Exception:
		return &arg
	case error:
		return normalizeError(arg)
	case string:
		ex := exception.InternalServerErrorException(arg)
		return &ex
	}

	ex := exception.InternalServerErrorException(http.StatusText(http.StatusInternalServerError))
	return &ex
}

func normalizeError(err error) *exception.Exception {
	if canWrap(err) {
		var pointer *exception.Exception
		if errors.As(err, &pointer) && pointer != nil {
			return pointer
		}
		var value exception.Exception
		if errors.As(err, &value) {
			return &value
		}
	}
	ex := exception.InternalServerErrorException(err.Error())
	return &ex
}

func canWrap(err error) bool {
	switch err.(type) {
	case interface{ Unwrap() error }, interface{ Unwrap() []error }, interface{ As(any) bool }:
		return true
	}
	return false
}
