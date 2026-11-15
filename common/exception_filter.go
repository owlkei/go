package common

import (
	"errors"
	"net/http"
	"reflect"

	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/color"
)

const ExceptionFilterMethodName = "Catch"

type ExceptionFilterable any

type ExceptionFilterItem struct {
	HTTP HTTPExceptionFilterItem
	WS   WSExceptionFilterItem
}

type exceptionFilterHandler struct {
	exceptionFilterable ExceptionFilterable
	handlers            []any
}

type ExceptionFilter struct {
	ExceptionFilterHandlers []exceptionFilterHandler
}

func (e *ExceptionFilter) BindExceptionFilter(exceptionFilterable ExceptionFilterable, handlers ...any) *ExceptionFilter {
	exceptionFilterHandler := exceptionFilterHandler{
		exceptionFilterable: exceptionFilterable,
		handlers:            handlers,
	}

	e.ExceptionFilterHandlers = append(e.ExceptionFilterHandlers, exceptionFilterHandler)
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
